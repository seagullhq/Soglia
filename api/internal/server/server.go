// Package server wires the HTTP API: routing, handlers, the files browser and
// the terminal. It depends on the auth, config, httpx, model, store and worker
// packages but nothing depends on it except the cmd/api entry point.
package server

import (
	"bufio"
	"context"
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

	// terminalRunning lets /terminal/cancel interrupt an in-flight shell
	// command. Keys are client-generated ids so concurrent commands stay separate.
	terminalMu      sync.Mutex
	terminalRunning map[string]context.CancelFunc
}

// New builds the fully routed API handler, including auth middleware, HSTS and
// CORS. The caller owns the http.Server and the shutdown lifecycle.
func New(w *worker.Client, opts config.Options, db *store.Store, mgr *auth.Manager) http.Handler {
	app := &api{
		worker:          w,
		opts:            opts,
		model:           loadModelConfig(opts.ModelDir),
		store:           db,
		auth:            mgr,
		terminalRunning: make(map[string]context.CancelFunc),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleRoot)
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
	mux.Handle("/users", mgr.RequirePerm(auth.PermUsers, http.HandlerFunc(app.handleUsers)))
	// Accept both /files and /files/ so a stray trailing slash cannot fall
	// through to the root handler and surface as a 404 in the console.
	mux.Handle("/files", mgr.RequirePerm(auth.PermFiles, http.HandlerFunc(app.handleFiles)))
	mux.Handle("/files/", mgr.RequirePerm(auth.PermFiles, http.HandlerFunc(app.handleFiles)))
	mux.Handle("/terminal", mgr.RequirePerm(auth.PermTerminal, http.HandlerFunc(app.handleTerminal)))
	mux.Handle("/terminal/cancel", mgr.RequirePerm(auth.PermTerminal, http.HandlerFunc(app.handleTerminalCancel)))
	mux.Handle("/moderate", mgr.RequirePerm(auth.PermModerate, http.HandlerFunc(app.handleModerate)))
	mux.Handle("/moderate/batch", mgr.RequirePerm(auth.PermModerate, http.HandlerFunc(app.handleBatch)))

	if webDir := config.FirstExistingDir(
		filepath.Join(config.ExecutableDir(), "ui", "dist"),
		filepath.Join(opts.ModelDir, "..", "api", "ui", "dist"),
		filepath.Join("ui", "dist"),
	); webDir != "" {
		mux.Handle("/ui/", uiHandler(webDir))
		slog.Info("management UI available", "url", "/ui/")
	}

	return httpx.WithHSTS(httpx.WithCORS(mux))
}

func (a *api) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		httpx.WriteError(w, http.StatusNotFound, "not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"service": "profanity-filter-api",
		"model":   a.opts.ModelDir,
		"endpoints": map[string]string{
			"GET  /health":          "worker status",
			"GET  /config":          "model settings",
			"GET  /stats":           "usage counters",
			"GET  /history":         "recent checks from the database",
			"DELETE /history":       "clear check history",
			"GET  /reviews":         "review queue with decisions",
			"POST /reviews":         `{"id": 1, "decision": "allow", "note": "..."}`,
			"DELETE /reviews?id=1":  "reopen a review by clearing its decision",
			"GET  /db":              "database tables and row counts",
			"GET  /db/table":        "?name=checks rows from one table",
			"POST /db/query":        `{"sql": "SELECT ..."}`,
			"GET  /users":           "console accounts (admin only)",
			"POST /users":           `{"username": "mod", "password": "...", "role": "moderator"}`,
			"PUT  /users":           `{"username": "mod", "role": "viewer", "enabled": true}`,
			"DELETE /users":         "?username=mod remove an account",
			"GET  /setup":           "account and service settings",
			"PUT  /setup":           "update setup",
			"GET  /files":           "server filesystem browser",
			"POST /terminal":        `{"command": "...", "cwd": "..."}`,
			"POST /terminal/cancel": `{"id": "..."}`,
			"POST /moderate":        `{"text": "message"}`,
			"POST /moderate/batch":  `{"texts": ["message", ...]}`,
		},
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
	httpx.WriteJSON(w, http.StatusOK, a.model)
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
	ModelDir        string       `json:"model_dir"`
	Temperature     float64      `json:"temperature"`
	BlockThreshold  float64      `json:"block_threshold"`
	ReviewThreshold float64      `json:"review_threshold"`
	BlocklistSize   int          `json:"blocklist_size"`
	MaxTokens       int          `json:"max_tokens"`
	Model           modelDetails `json:"model"`
	Files           []modelFile  `json:"files"`
}

func loadModelConfig(dir string) *modelConfig {
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

// uiHandler serves the built React app under /ui/. Unknown paths fall back to
// index.html so client-side routing keeps working.
func uiHandler(root string) http.Handler {
	index := filepath.Join(root, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/ui"), "/")
		if rel == "" {
			rel = "index.html"
		}
		target := filepath.Join(root, filepath.Clean(rel))
		if strings.HasPrefix(target, "..") || !strings.HasPrefix(target, filepath.Clean(root)+string(os.PathSeparator)) && target != index {
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
