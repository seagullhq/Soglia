// Package worker owns the long-lived Python inference process and speaks
// JSON-lines to it over stdin/stdout.
package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"profanity-filter-api/internal/model"
)

type workerResponse struct {
	ID      int     `json:"id"`
	Action  string  `json:"action"`
	Tier    string  `json:"tier"`
	Prob    float64 `json:"prob"`
	MS      float64 `json:"ms"`
	Matched string  `json:"matched"`
	Error   string  `json:"error"`
}

type workerRequest struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

// Client owns one Python worker. All requests are serialized because the ONNX
// session inside the worker is single-threaded.
type Client struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	waitCh   chan struct{}
	modelDir string

	mu      sync.Mutex
	nextID  int
	pending map[int]chan workerResponse

	done    chan struct{}
	exitMu  sync.Mutex
	exitErr error
}

// NewClient starts the worker and begins reading its responses.
func NewClient(python, worker, modelDir string) (*Client, error) {
	cmd := exec.Command(python, worker, modelDir)
	cmd.Stderr = os.Stderr // Python diagnostics must never mix with stdout JSON

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s %s: %w", python, worker, err)
	}

	c := &Client{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		waitCh:   make(chan struct{}),
		modelDir: modelDir,
		pending:  make(map[int]chan workerResponse),
		done:     make(chan struct{}),
	}
	go c.readLoop()
	go func() {
		err := cmd.Wait()
		c.setExitErr(err)
		close(c.waitCh)
	}()
	return c, nil
}

// Done is closed if the worker exits, so the health endpoint can report a
// degraded service.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var resp workerResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			slog.Warn("ignoring invalid worker response", "line", string(line), "error", err)
			continue
		}
		c.mu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Error("worker output reader stopped", "error", err)
	}
}

func (c *Client) setExitErr(err error) {
	c.exitMu.Lock()
	if c.exitErr == nil {
		if err == nil {
			c.exitErr = errors.New("inference worker exited unexpectedly")
		} else {
			c.exitErr = fmt.Errorf("inference worker exited: %w", err)
		}
	}
	c.exitMu.Unlock()
	close(c.done)
}

func (c *Client) currentErr() error {
	c.exitMu.Lock()
	defer c.exitMu.Unlock()
	return c.exitErr
}

func (c *Client) removePending(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Moderate sends one text to the worker and waits for its verdict.
func (c *Client) Moderate(ctx context.Context, text string) (*model.ModerationResult, error) {
	select {
	case <-c.done:
		return nil, c.currentErr()
	default:
	}

	c.mu.Lock()
	id := c.nextID
	c.nextID++
	ch := make(chan workerResponse, 1)
	c.pending[id] = ch
	payload, err := json.Marshal(workerRequest{ID: id, Text: text})
	if err == nil {
		_, err = fmt.Fprintf(c.stdin, "%s\n", payload)
	}
	c.mu.Unlock()
	if err != nil {
		c.removePending(id)
		return nil, fmt.Errorf("send to worker: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
		return &model.ModerationResult{
			Action:  resp.Action,
			Tier:    resp.Tier,
			Prob:    resp.Prob,
			MS:      resp.MS,
			Matched: resp.Matched,
		}, nil
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-c.done:
		c.removePending(id)
		return nil, c.currentErr()
	}
}

// Close shuts down the worker, killing it if it does not exit on EOF.
func (c *Client) Close() error {
	_ = c.stdin.Close()
	select {
	case <-c.waitCh:
	case <-time.After(2 * time.Second):
		_ = c.cmd.Process.Kill()
		<-c.waitCh
	}
	return nil
}
