// Package server wires the HTTP API: routing, handlers, the files browser and
// the terminal. It depends on the auth, config, httpx, model, store and worker
// packages but nothing depends on it except the cmd/api entry point.
package server

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/config"
	"profanity-filter-api/internal/httpx"
	"profanity-filter-api/internal/model"
	"profanity-filter-api/internal/store"
	"profanity-filter-api/internal/worker"
)

// api is the handler receiver shared by all server files.
type api struct {
	worker *worker.Client
	opts   config.Options
	model  *modelConfig
	store  *store.Store
	auth   *auth.Manager

	// Terminal state tracks the read-only diagnostic terminal. Commands are
	// bound to the session that started them, and usage is tracked per session
	// so one account cannot monopolise the endpoint.
	terminalMu      sync.Mutex
	terminalRunning map[string]runningTerminal // session\x00client id -> command
	terminalActive  map[string]int             // session -> running commands
	terminalUsage   map[string]terminalUsage   // session -> rate-limit window
}

// New builds the fully routed API handler, including auth middleware, HSTS and
// CORS. The caller owns the http.Server and the shutdown lifecycle.
func New(w *worker.Client, opts config.Options, db *store.Store, mgr *auth.Manager) http.Handler {
	app := &api{
		worker:          w,
		opts:            opts,
		model:           loadModelConfig(opts.ModelDir, opts.CalibrationOverride),
		store:           db,
		auth:            mgr,
		terminalRunning: make(map[string]runningTerminal),
		terminalActive:  make(map[string]int),
		terminalUsage:   make(map[string]terminalUsage),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/info", app.handleRoot)
	mux.HandleFunc("/health", app.handleHealth)
	mux.HandleFunc("/auth/login", mgr.HandleLogin)
	mux.HandleFunc("/auth/logout", mgr.HandleLogout)
	mux.HandleFunc("/auth/me", mgr.HandleMe)
	mux.Handle("/config", mgr.Require(http.HandlerFunc(app.handleConfig)))
	mux.Handle("/stats", mgr.Require(http.HandlerFunc(app.handleStats)))
	mux.Handle("/history", mgr.Require(http.HandlerFunc(app.handleHistory)))
	mux.Handle("/reviews", mgr.RequirePerm(auth.PermReview, http.HandlerFunc(app.handleReviews)))
	mux.Handle("/db", mgr.RequirePerm(auth.PermDB, http.HandlerFunc(app.handleDB)))
	mux.Handle("/db/table", mgr.RequirePerm(auth.PermDB, http.HandlerFunc(app.handleDBTable)))
	mux.Handle("/db/query", mgr.RequirePerm(auth.PermDB, http.HandlerFunc(app.handleDBQuery)))
	mux.Handle("/setup", mgr.RequirePerm(auth.PermSetup, http.HandlerFunc(app.handleSetup)))
	mux.Handle("/setup/reset", mgr.RequirePerm(auth.PermSetup, http.HandlerFunc(app.handleSetupReset)))
	mux.Handle("/users", mgr.RequirePerm(auth.PermUsers, http.HandlerFunc(app.handleUsers)))
	// Accept both /files and /files/ so a stray trailing slash cannot fall
	// through to the root handler and surface as a 404 in the console.
	mux.Handle("/files", mgr.RequirePerm(auth.PermFiles, http.HandlerFunc(app.handleFiles)))
	mux.Handle("/files/", mgr.RequirePerm(auth.PermFiles, http.HandlerFunc(app.handleFiles)))
	if opts.TerminalEnabled {
		mux.Handle("/terminal", mgr.RequirePerm(auth.PermTerminal, http.HandlerFunc(app.handleTerminal)))
		mux.Handle("/terminal/cancel", mgr.RequirePerm(auth.PermTerminal, http.HandlerFunc(app.handleTerminalCancel)))
		slog.Info("diagnostic terminal enabled", "mode", "read-only allow-list")
	} else {
		// Explicit 404s stop POSTs falling through to the UI catch-all handler.
		mux.Handle("/terminal", http.NotFoundHandler())
		mux.Handle("/terminal/cancel", http.NotFoundHandler())
		slog.Info("diagnostic terminal disabled")
	}
	mux.Handle("/moderate", mgr.RequirePerm(auth.PermModerate, http.HandlerFunc(app.handleModerate)))
	mux.Handle("/moderate/batch", mgr.RequirePerm(auth.PermModerate, http.HandlerFunc(app.handleBatch)))

	if webDir := config.FirstExistingDir(
		filepath.Join(config.ExecutableDir(), "ui", "dist"),
		filepath.Join(opts.ModelDir, "..", "api", "ui", "dist"),
		filepath.Join("ui", "dist"),
	); webDir != "" {
		// The console owns the site root. /ui/ stays as an alias so existing
		// bookmarks and cached HTML keep working.
		ui := uiHandler(webDir)
		mux.Handle("/", ui)
		mux.Handle("/ui/", ui)
		slog.Info("management UI available", "url", "/")
	} else {
		mux.HandleFunc("/", app.handleRoot)
	}

	return httpx.WithHSTS(httpx.WithCORS(mux))
}

// handleRoot serves the small JSON service description. It is reachable at
// /info; if no UI build is present it also answers at /.
func (a *api) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/info" {
		httpx.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	endpoints := map[string]string{
		"GET  /info":           "this service description",
		"GET  /health":         "worker status",
		"GET  /config":         "model settings",
		"GET  /stats":          "usage counters",
		"GET  /history":        "recent checks from the database",
		"DELETE /history":      "clear check history",
		"GET  /reviews":        "review queue with decisions",
		"POST /reviews":        `{"id": 1, "decision": "allow", "note": "..."}`,
		"DELETE /reviews?id=1": "reopen a review by clearing its decision",
		"GET  /db":             "database tables and row counts",
		"GET  /db/table":       "?name=checks rows from one table",
		"POST /db/query":       `{"sql": "SELECT ..."}`,
		"GET  /users":          "console accounts (admin only)",
		"POST /users":          `{"username": "mod", "password": "...", "role": "moderator"}`,
		"PUT  /users":          `{"username": "mod", "role": "viewer", "enabled": true}`,
		"DELETE /users":        "?username=mod remove an account",
		"GET  /setup":          "account and service settings",
		"PUT  /setup":          "update setup",
		"GET  /files":          "server filesystem browser",
		"POST /moderate":       `{"text": "message"}`,
		"POST /moderate/batch": `{"texts": ["message", ...]}`,
	}
	if a.opts.TerminalEnabled {
		endpoints["POST /terminal"] = `{"command": "ls -la", "cwd": "/app"} (read-only)`
		endpoints["POST /terminal/cancel"] = `{"id": "..."}`
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"service":          "profanity-filter-api",
		"model":            a.opts.ModelDir,
		"terminal_enabled": a.opts.TerminalEnabled,
		"endpoints":        endpoints,
	})
}

func (a *api) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	status := "ok"
	code := http.StatusOK
	select {
	case <-a.worker.Done():
		status = "degraded: worker stopped"
		code = http.StatusServiceUnavailable
	default:
	}
	httpx.WriteJSON(w, code, map[string]any{
		"status": status,
		"model":  a.opts.ModelDir,
	})
}

func (a *api) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	// Copy so the reported flags do not mutate the shared model config.
	response := *a.model
	response.TerminalEnabled = a.opts.TerminalEnabled
	httpx.WriteJSON(w, http.StatusOK, response)
}

type modelDetails struct {
	Architecture        string `json:"architecture"`
	Task                string `json:"task"`
	Layers              int    `json:"layers"`
	Heads               int    `json:"heads"`
	HiddenSize          int    `json:"hidden_size"`
	VocabSize           int    `json:"vocab_size"`
	TransformersVersion string `json:"transformers_version"`
}

type modelFile struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

type modelConfig struct {
	TerminalEnabled bool         `json:"terminal_enabled"`
	ModelDir        string       `json:"model_dir"`
	Temperature     float64      `json:"temperature"`
	BlockThreshold  float64      `json:"block_threshold"`
	ReviewThreshold float64      `json:"review_threshold"`
	BlocklistSize   int          `json:"blocklist_size"`
	MaxTokens       int          `json:"max_tokens"`
	Model           modelDetails `json:"model"`
	Files           []modelFile  `json:"files"`
}

func loadModelConfig(dir string, override model.CalibrationOverride) *modelConfig {
	// MaxTokens mirrors tokenizer.enable_truncation(max_length=64) in worker.py.
	cfg := &modelConfig{ModelDir: dir, MaxTokens: 64}

	if data, err := os.ReadFile(filepath.Join(dir, "calibration.json")); err == nil {
		var raw struct {
			Temperature     float64 `json:"temperature"`
			BlockThreshold  float64 `json:"block_threshold"`
			ReviewThreshold float64 `json:"review_threshold"`
		}
		if err := json.Unmarshal(data, &raw); err == nil {
			cfg.Temperature = raw.Temperature
			cfg.BlockThreshold = raw.BlockThreshold
			cfg.ReviewThreshold = raw.ReviewThreshold
		}
	}
	if override.Temperature != nil {
		cfg.Temperature = *override.Temperature
	}
	if override.BlockThreshold != nil {
		cfg.BlockThreshold = *override.BlockThreshold
	}
	if override.ReviewThreshold != nil {
		cfg.ReviewThreshold = *override.ReviewThreshold
	}

	if data, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		var raw struct {
			Architectures       []string `json:"architectures"`
			ProblemType         string   `json:"problem_type"`
			Layers              int      `json:"num_hidden_layers"`
			Heads               int      `json:"num_attention_heads"`
			HiddenSize          int      `json:"hidden_size"`
			VocabSize           int      `json:"vocab_size"`
			TransformersVersion string   `json:"transformers_version"`
		}
		if err := json.Unmarshal(data, &raw); err == nil {
			if len(raw.Architectures) > 0 {
				cfg.Model.Architecture = raw.Architectures[0]
				if strings.Contains(raw.Architectures[0], "XLMRoberta") {
					cfg.Model.Architecture = "XLM-RoBERTa"
				}
			}
			switch raw.ProblemType {
			case "single_label_classification":
				cfg.Model.Task = "Single-label classification"
			case "multi_label_classification":
				cfg.Model.Task = "Multi-label classification"
			default:
				cfg.Model.Task = raw.ProblemType
			}
			cfg.Model.Layers = raw.Layers
			cfg.Model.Heads = raw.Heads
			cfg.Model.HiddenSize = raw.HiddenSize
			cfg.Model.VocabSize = raw.VocabSize
			cfg.Model.TransformersVersion = raw.TransformersVersion
		}
	}

	f, err := os.Open(filepath.Join(dir, "clean_blocklist.txt"))
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) != "" {
				cfg.BlocklistSize++
			}
		}
	}

	for _, name := range []string{
		"model_int8.onnx", "tokenizer.json", "clean_blocklist.txt",
		"config.json", "calibration.json", "model.safetensors", "model_fp32.onnx.data",
	} {
		if st, err := os.Stat(filepath.Join(dir, name)); err == nil && !st.IsDir() {
			cfg.Files = append(cfg.Files, modelFile{Name: name, Bytes: st.Size()})
		}
	}
	return cfg
}

// uiHandler serves the built React app at the site root. /ui/ is accepted as
// an alias, and unknown paths fall back to index.html for client-side routing.
func uiHandler(root string) http.Handler {
	index := filepath.Join(root, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "ui" {
			rel = ""
		} else if strings.HasPrefix(rel, "ui/") {
			rel = strings.TrimPrefix(rel, "ui/")
		}
		if rel == "" {
			rel = "index.html"
		}
		target := filepath.Join(root, filepath.Clean(rel))
		cleanRoot := filepath.Clean(root)
		if target != cleanRoot && !strings.HasPrefix(target, cleanRoot+string(os.PathSeparator)) {
			http.NotFound(w, r)
			return
		}
		if st, err := os.Stat(target); err != nil || st.IsDir() {
			target = index
		}
		if _, err := os.Stat(target); err != nil {
			http.NotFound(w, r)
			return
		}
		// A cached index.html can point at asset hashes that no longer exist,
		// so never let the browser keep any UI response.
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.ServeFile(w, r, target)
	})
}
