// Command profanity-filter-api exposes the v6 profanity-filter model as a small
// HTTP service. The model itself runs in a long-lived Python worker (see
// worker.py) because ONNX Runtime / Hugging Face tokenizers are Python
// libraries; this process owns the worker and speaks JSON-lines to it.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"profanity-filter-api/internal/auth"
	"profanity-filter-api/internal/config"
	"profanity-filter-api/internal/server"
	"profanity-filter-api/internal/store"
	"profanity-filter-api/internal/worker"
)

func main() {
	opts := config.ParseOptions()
	if (opts.TLSCert == "") != (opts.TLSKey == "") {
		config.Fatalf("-tls-cert and -tls-key must be set together to enable HTTPS")
	}
	config.EnsureSettingsFile(opts)

	client, err := worker.NewClient(opts.Python, opts.Worker, opts.ModelDir)
	if err != nil {
		slog.Error("could not start inference worker", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	db, err := store.Open(opts.DBURL)
	if err != nil {
		slog.Error("could not open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// HTTPS always implies Secure cookies, even if the flag/file says otherwise.
	secureCookie := opts.SecureCookie || opts.TLSEnabled()
	mgr, err := auth.New(db, secureCookie)
	if err != nil {
		slog.Error("could not set up authentication", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           server.New(client, opts, db, mgr),
		ReadHeaderTimeout: 5 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-shutdown
		slog.Info("shutdown requested")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	scheme := "http"
	if opts.TLSEnabled() {
		scheme = "https"
		slog.Info("HTTPS enabled", "cert", opts.TLSCert, "secure_cookies", secureCookie)
	}
	slog.Info("API listening", "scheme", scheme, "addr", opts.Addr, "model", opts.ModelDir)
	if opts.TLSEnabled() {
		err = srv.ListenAndServeTLS(opts.TLSCert, opts.TLSKey)
	} else {
		err = srv.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
	slog.Info("stopped")
}
