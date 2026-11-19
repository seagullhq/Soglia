package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/httpx"
)

const (
	terminalTimeout    = 15 * time.Second
	maxTerminalCommand = 8 << 10
	maxTerminalOutput  = 64 << 10
	terminalCwdMarker  = "__SOGLIA_CWD__"
)

type terminalRequest struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
	ID      string `json:"id,omitempty"`
}

type terminalResponse struct {
	Output string `json:"output"`
	Cwd    string `json:"cwd"`
	Code   int    `json:"code"`
}

// handleTerminal runs a single shell command for a signed-in operator. A marker
// appended to the script reports the directory after the command, so `cd`
// persists when the UI sends the returned cwd with the next command.
func (a *api) handleTerminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req terminalRequest
	if err := httpx.DecodeBody(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		httpx.WriteError(w, http.StatusBadRequest, "command is required")
		return
	}
	if len(command) > maxTerminalCommand {
		httpx.WriteError(w, http.StatusBadRequest, "command is too long")
		return
	}

	cwd, err := resolveFilePath(req.Cwd)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		httpx.WriteError(w, http.StatusBadRequest, "working directory not found")
		return
	}

	shell, shellFlag := "sh", "-c"
	cwdMarker := "printf '\\n" + terminalCwdMarker + ":%s\\n' \"$(pwd)\""
	if runtime.GOOS == "windows" {
		shell, shellFlag = "cmd", "/C"
		cwdMarker = "echo " + terminalCwdMarker + ":%CD%"
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTimeout)
	defer cancel()
	// Publish the cancel function while the command runs so /terminal/cancel
	// can interrupt it from a separate HTTP request.
	if req.ID != "" {
		a.terminalMu.Lock()
		a.terminalRunning[req.ID] = cancel
		a.terminalMu.Unlock()
		defer func() {
			a.terminalMu.Lock()
			delete(a.terminalRunning, req.ID)
			a.terminalMu.Unlock()
		}()
	}

	cmd := exec.CommandContext(ctx, shell, shellFlag, command+"\n"+cwdMarker)
	cmd.Dir = cwd
	// Safety net: never let an orphaned child hold the output pipe open forever.
	cmd.WaitDelay = 500 * time.Millisecond
	configureTerminalProcess(cmd)
	started := time.Now()
	output, err := cmd.CombinedOutput()

	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		code = -1
		output = append(output, []byte("\ncommand timed out after "+terminalTimeout.String())...)
	case errors.Is(ctx.Err(), context.Canceled):
		// 130 is the conventional shell code for interruption (SIGINT).
		code = 130
		output = append(output, []byte("\ncommand cancelled")...)
	}
	slog.Info("terminal command",
		"user", auth.CurrentUser(r).Username,
		"command", command,
		"cwd", cwd,
		"code", code,
		"ms", time.Since(started).Milliseconds(),
	)

	text := string(output)
	if idx := strings.LastIndex(text, terminalCwdMarker+":"); idx >= 0 {
		tail := text[idx+len(terminalCwdMarker)+1:]
		if end := strings.IndexAny(tail, "\r\n"); end >= 0 {
			if next := strings.TrimSpace(tail[:end]); next != "" {
				cwd = next
			}
		}
		text = strings.TrimRight(text[:idx], "\r\n")
	}
	if len(text) > maxTerminalOutput {
		text = text[:maxTerminalOutput] + "\n… output truncated"
	}

	httpx.WriteJSON(w, http.StatusOK, terminalResponse{Output: text, Cwd: cwd, Code: code})
}

// handleTerminalCancel interrupts the command registered under the given id.
// A missing id simply means the command already finished; reporting that as a
// benign result keeps the Stop button idempotent.
func (a *api) handleTerminalCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := httpx.DecodeBody(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "id is required")
		return
	}

	a.terminalMu.Lock()
	cancel, ok := a.terminalRunning[req.ID]
	a.terminalMu.Unlock()
	if !ok {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"cancelled": false})
		return
	}
	cancel()
	slog.Info("terminal command cancelled", "user", auth.CurrentUser(r).Username, "id", req.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"cancelled": true})
}
