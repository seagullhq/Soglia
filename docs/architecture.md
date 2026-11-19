# Architecture

← [API documentation index](../api/README.md)

A small HTTP API in Go that serves the profanity-filter model in `../v6`, plus a
React management console served by the same process.
The Go process starts one long-lived Python worker (`worker.py`) which loads the
model once and runs inference; requests are sent to it over JSON-lines on stdin.

> **Model availability:** the trained `v6` model is not included in this
> repository and will be released later. To run the service, place the model at
> the configured `model_dir` (`/app/v6` in the container, `../v6` for local
> development).

Architecture:

```
browser ──HTTPS──> Go API (cmd/api) ──TLS──> PostgreSQL
                        │
                        └──JSON-lines (local pipes)──> Python worker ──> v6 ONNX model
```

## Go layout

The server follows the conventional Go module layout: a thin entry point in
`cmd/api` owns flags, startup and shutdown, while the logic lives in internal
packages that cannot be imported from outside this module:

```
api/
├── cmd/
│   ├── api/              API server entry point (build with ./cmd/api)
│   ├── gen-certs/        local CA and TLS certificate generator
│   └── migrate-sqlite/   one-time SQLite → Postgres migration
├── internal/
│   ├── config/           flags, env vars, settings.json, path detection
│   ├── httpx/            shared JSON/CORS/HSTS helpers
│   ├── auth/             roles, permissions, sessions, login handlers
│   ├── store/            Postgres persistence and embedded migrations
│   ├── model/            shared moderation verdict type
│   ├── worker/           Python worker process and JSON-lines client
│   └── server/           routing and all HTTP handlers, files browser, terminal
├── ui/                   React + Vite console (pages/, components/, lib/, styles/)
└── worker.py             Python ONNX inference worker
```

`internal/auth` declares the small `Store` interface it needs, so the Postgres
implementation in `internal/store` can depend on auth without an import cycle.
Permissions are checked in the `auth` middleware on every protected route; no
UI state is trusted for authorization.

Both network links are encrypted: the browser talks to the Go API over HTTPS,
and the API talks to PostgreSQL over TLS with full certificate verification.
The worker is a child process connected by stdin/stdout pipes, not a network
socket.

The Go API uses the standard library plus `github.com/jackc/pgx/v5` to talk to
PostgreSQL. The worker needs Python with `onnxruntime`, `tokenizers`, and
`numpy` installed (already present here).
