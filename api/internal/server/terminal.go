package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/config"
	"profanity-filter-api/internal/httpx"
)

const (
	terminalTimeout    = 15 * time.Second
	maxTerminalCommand = 8 << 10
	maxTerminalOutput  = 64 << 10
	terminalRateLimit  = 30
	terminalRateWindow = time.Minute
	maxTerminalID      = 128
)

var errTerminalOutputLimit = errors.New("terminal output limit reached")

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

type runningTerminal struct {
	owner  string
	cancel context.CancelFunc
}

type terminalUsage struct {
	window time.Time
	count  int
}

// handleTerminal runs one read-only diagnostic command for an enabled admin.
// There is no shell: the command is split into words, the program must be on a
// trusted allow-list, and every file argument is confined to /app. cd is
// handled here (it only changes the returned cwd) so no write-capable process
// is ever started.
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
	if len(req.ID) > maxTerminalID {
		httpx.WriteError(w, http.StatusBadRequest, "id is too long")
		return
	}

	user := auth.CurrentUser(r).Username
	session := auth.SessionKey(r)
	secrets := terminalSecrets(a.opts)
	auditCommand := redactTerminalSecrets(command, secrets)

	if err := a.terminalBegin(session); err != nil {
		slog.Warn("terminal request throttled",
			"user", user, "session", session, "ip", r.RemoteAddr, "reason", err.Error())
		httpx.WriteError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	defer a.terminalEnd(session)

	cwd, err := resolveTerminalPath(filesRoot, req.Cwd)
	if err != nil {
		rejectTerminalCommand(w, r, user, session, auditCommand, cwd, err)
		return
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		rejectTerminalCommand(w, r, user, session, auditCommand, cwd, errors.New("working directory not found"))
		return
	}

	words, err := splitTerminalCommand(command)
	if err != nil {
		rejectTerminalCommand(w, r, user, session, auditCommand, cwd, err)
		return
	}
	if len(words) == 0 {
		rejectTerminalCommand(w, r, user, session, auditCommand, cwd, errors.New("command is required"))
		return
	}
	program, args := words[0], words[1:]

	// cd needs no process: validate the target and return the new directory.
	if program == "cd" {
		if len(args) > 1 {
			rejectTerminalCommand(w, r, user, session, auditCommand, cwd, errors.New("cd accepts at most one path"))
			return
		}
		next := cwd
		if len(args) == 1 {
			next, err = resolveTerminalPath(cwd, args[0])
			if err != nil {
				rejectTerminalCommand(w, r, user, session, auditCommand, cwd, err)
				return
			}
		}
		if info, err := os.Stat(next); err != nil || !info.IsDir() {
			rejectTerminalCommand(w, r, user, session, auditCommand, cwd, errors.New("working directory not found"))
			return
		}
		slog.Info("terminal command",
			"user", user, "session", session, "ip", r.RemoteAddr,
			"command", auditCommand, "cwd", cwd, "code", 0,
			"bytes", 0, "ms", 0, "outcome", "cd")
		httpx.WriteJSON(w, http.StatusOK, terminalResponse{Cwd: next})
		return
	}

	if err := terminalValidateArgs(program, args); err != nil {
		rejectTerminalCommand(w, r, user, session, auditCommand, cwd, err)
		return
	}
	for _, candidate := range terminalPathArgs(args) {
		if err := checkTerminalPath(cwd, candidate); err != nil {
			rejectTerminalCommand(w, r, user, session, auditCommand, cwd, err)
			return
		}
	}
	programPath, err := trustedProgramPath(program)
	if err != nil {
		rejectTerminalCommand(w, r, user, session, auditCommand, cwd, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), terminalTimeout)
	defer cancel()
	// Publish the cancel function under a session-scoped key so another login
	// cannot interrupt this command, even if it guesses the client id.
	a.terminalRegister(session, req.ID, cancel)
	defer a.terminalUnregister(session, req.ID)

	cmd := exec.CommandContext(ctx, programPath, args...)
	cmd.Dir = cwd
	cmd.Env = terminalEnv(cwd)
	// Safety net: never let an orphaned child hold the output pipe open forever.
	cmd.WaitDelay = 500 * time.Millisecond
	configureTerminalProcess(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		rejectTerminalCommand(w, r, user, session, auditCommand, cwd, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		rejectTerminalCommand(w, r, user, session, auditCommand, cwd, err)
		return
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		slog.Warn("terminal command could not start",
			"user", user, "session", session, "ip", r.RemoteAddr,
			"command", auditCommand, "cwd", cwd, "reason", err.Error())
		httpx.WriteError(w, http.StatusInternalServerError, "could not start command")
		return
	}

	// Bound output while it streams. CombinedOutput would keep growing in
	// memory until the 15 s timeout, so a noisy command could exhaust the host.
	var out cappedBuffer
	out.onLimit = cancel
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		_, _ = io.Copy(&out, stdout)
	}()
	go func() {
		defer readers.Done()
		_, _ = io.Copy(&out, stderr)
	}()
	readers.Wait()
	waitErr := cmd.Wait()

	code := 0
	outcome := "ok"
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	text := redactTerminalSecrets(out.String(), secrets)
	switch {
	case out.limitHit():
		code = -1
		outcome = "output_limit"
		text += "\noutput limit reached (" + fmt.Sprintf("%d", maxTerminalOutput) + " bytes)"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		code = -1
		outcome = "timeout"
		text += "\ncommand timed out after " + terminalTimeout.String()
	case errors.Is(ctx.Err(), context.Canceled):
		// 130 is the conventional shell code for interruption (SIGINT).
		code = 130
		outcome = "cancelled"
		text += "\ncommand cancelled"
	}

	slog.Info("terminal command",
		"user", user, "session", session, "ip", r.RemoteAddr,
		"command", auditCommand,
		"program", programPath,
		"args", redactTerminalArgs(args, secrets),
		"cwd", cwd,
		"code", code,
		"bytes", len(text),
		"ms", time.Since(started).Milliseconds(),
		"outcome", outcome,
	)
	httpx.WriteJSON(w, http.StatusOK, terminalResponse{Output: text, Cwd: cwd, Code: code})
}

// handleTerminalCancel interrupts only a command registered by this session.
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
	if len(req.ID) > maxTerminalID {
		httpx.WriteError(w, http.StatusBadRequest, "id is too long")
		return
	}

	session := auth.SessionKey(r)
	key := terminalKey(session, req.ID)
	a.terminalMu.Lock()
	running, ok := a.terminalRunning[key]
	a.terminalMu.Unlock()
	if !ok || running.owner != session {
		// A missing or foreign id is benign: the Stop button stays idempotent.
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"cancelled": false})
		return
	}
	running.cancel()
	slog.Info("terminal command cancelled",
		"user", auth.CurrentUser(r).Username,
		"session", session, "ip", r.RemoteAddr, "id", req.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"cancelled": true})
}

func terminalKey(session, id string) string {
	return session + "\x00" + id
}

func (a *api) terminalRegister(session, id string, cancel context.CancelFunc) {
	if id == "" {
		return
	}
	a.terminalMu.Lock()
	a.terminalRunning[terminalKey(session, id)] = runningTerminal{owner: session, cancel: cancel}
	a.terminalMu.Unlock()
}

func (a *api) terminalUnregister(session, id string) {
	if id == "" {
		return
	}
	a.terminalMu.Lock()
	delete(a.terminalRunning, terminalKey(session, id))
	a.terminalMu.Unlock()
}

// terminalBegin applies a per-session rate limit and keeps a single command in
// flight per session. Stale rate windows are pruned so the map cannot grow
// forever with ids of sessions that never return.
func (a *api) terminalBegin(session string) error {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()
	now := time.Now()
	usage := a.terminalUsage[session]
	if now.Sub(usage.window) >= terminalRateWindow {
		usage = terminalUsage{window: now}
	}
	usage.count++
	a.terminalUsage[session] = usage
	if usage.count > terminalRateLimit {
		return errors.New("too many terminal requests; slow down")
	}
	if a.terminalActive[session] >= 1 {
		return errors.New("a terminal command is already running for this session")
	}
	a.terminalActive[session]++
	if len(a.terminalUsage) > 512 {
		for key, value := range a.terminalUsage {
			if now.Sub(value.window) > 2*terminalRateWindow {
				delete(a.terminalUsage, key)
			}
		}
	}
	return nil
}

func (a *api) terminalEnd(session string) {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()
	if a.terminalActive[session] > 0 {
		a.terminalActive[session]--
	}
	if a.terminalActive[session] == 0 {
		delete(a.terminalActive, session)
	}
}

// splitTerminalCommand tokenises a command without a shell. Quotes only group
// words; they never expand variables or run subshells. Anything that would be
// interpreted by a shell is rejected outright.
func splitTerminalCommand(command string) ([]string, error) {
	var words []string
	var word strings.Builder
	var quote rune
	for _, char := range command {
		if char == 0 || char == '\n' || char == '\r' {
			return nil, errors.New("newlines and control characters are not allowed")
		}
		if unicode.IsControl(char) {
			return nil, errors.New("control characters are not allowed")
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case ' ', '\t':
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}
		case ';', '|', '&', '<', '>', '`', '$', '(', ')', '{', '}', '!', '\\':
			return nil, fmt.Errorf("shell operator %q is not allowed", string(char))
		default:
			word.WriteRune(char)
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	return words, nil
}

// terminalValidateArgs blocks options that can execute commands, write files
// or keep a command alive indefinitely, even though the programs themselves
// are otherwise read-only.
func terminalValidateArgs(program string, args []string) error {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		option := strings.ToLower(arg)
		if program == "find" {
			for _, forbidden := range []string{"-exec", "-ok", "-delete", "-fprint", "-fls", "-fprintf"} {
				if strings.HasPrefix(option, forbidden) {
					return fmt.Errorf("option %q is not allowed", arg)
				}
			}
		}
		if program == "tail" && (option == "-f" || option == "--follow" || strings.HasPrefix(option, "-f=")) {
			return errors.New("following a file is not allowed")
		}
	}
	return nil
}

// terminalPathArgs extracts candidate file paths from arguments, including
// the values of --option=value flags. Non-path arguments (grep patterns, for
// example) simply do not exist on disk and are left alone.
func terminalPathArgs(args []string) []string {
	var paths []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			if index := strings.IndexByte(arg, '='); index >= 0 && index+1 < len(arg) {
				paths = append(paths, arg[index+1:])
			}
			continue
		}
		paths = append(paths, arg)
	}
	return paths
}

func checkTerminalPath(cwd, raw string) error {
	if raw == "" {
		return nil
	}
	target := raw
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, raw)
	}
	if _, err := os.Stat(target); err != nil {
		return nil // a pattern or literal argument, not a path
	}
	resolved, err := resolveTerminalPath(cwd, raw)
	if err != nil {
		return err
	}
	if info, err := os.Stat(resolved); err == nil && !info.IsDir() {
		if locked, reason := fileLockReason(filepath.Base(resolved)); locked {
			return fmt.Errorf("reading this file is not allowed: %s", reason)
		}
	}
	return nil
}

// resolveTerminalPath resolves a terminal path against the given directory and
// applies the same /app confinement as the Files tab, including symlinks.
func resolveTerminalPath(base, raw string) (string, error) {
	target := raw
	if strings.TrimSpace(target) == "" {
		target = filesRoot
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(real)
	}
	if !withinFilesRoot(abs) {
		return "", fmt.Errorf("path must be inside %s", filesRoot)
	}
	return abs, nil
}

// terminalSecrets collects secret values the API process can see, so they can
// be scrubbed from command output and audit records.
func terminalSecrets(opts config.Options) []string {
	values := []string{
		os.Getenv("SOGLIA_PASSWORD"),
		os.Getenv("POSTGRES_PASSWORD"),
		os.Getenv("DATABASE_URL"),
		os.Getenv("SOGLIA_DATABASE_URL"),
		opts.DBURL,
	}
	if parsed, err := url.Parse(opts.DBURL); err == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			values = append(values, password)
		}
	}
	seen := make(map[string]bool)
	var secrets []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		// Short values would rewrite ordinary words all over the output.
		if len(value) < 6 || seen[value] {
			continue
		}
		seen[value] = true
		secrets = append(secrets, value)
	}
	return secrets
}

func redactTerminalSecrets(text string, secrets []string) string {
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, config.MaskedSecret)
	}
	return text
}

func redactTerminalArgs(args, secrets []string) []string {
	redacted := make([]string, len(args))
	for index, arg := range args {
		redacted[index] = redactTerminalSecrets(arg, secrets)
	}
	return redacted
}

type cappedBuffer struct {
	mu      sync.Mutex
	buf     []byte
	limited bool
	onLimit func()
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	room := maxTerminalOutput - len(b.buf)
	if room <= 0 {
		b.limited = true
		if b.onLimit != nil {
			b.onLimit()
		}
		return 0, errTerminalOutputLimit
	}
	written := len(p)
	if written > room {
		written = room
		b.limited = true
	}
	b.buf = append(b.buf, p[:written]...)
	if b.limited && b.onLimit != nil {
		b.onLimit()
	}
	if b.limited {
		return written, errTerminalOutputLimit
	}
	return written, nil
}

func (b *cappedBuffer) limitHit() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.limited
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func rejectTerminalCommand(w http.ResponseWriter, r *http.Request, user, session, command, cwd string, err error) {
	slog.Warn("terminal command rejected",
		"user", user, "session", session, "ip", r.RemoteAddr,
		"command", command, "cwd", cwd, "reason", err.Error())
	httpx.WriteError(w, http.StatusBadRequest, err.Error())
}
