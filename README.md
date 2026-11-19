# Soglia — Profanity Filter API & Management Console

Soglia is a self-hosted profanity-detection service built around a trained v6
ONNX model. A Go HTTP API wraps a long-lived Python inference worker (JSON-lines
over local pipes) and serves a React management console from the same process.
Browser traffic uses HTTPS; the API talks to PostgreSQL over TLS with full
certificate verification.

> **Model availability:** the trained `v6` model is not included in this
> repository and will be released later. To run the service, place the model at
> the configured `model_dir` (`/app/v6` in the container, `../v6` for local
> development).

## What it does

- **Moderation** — single-message and batch checks with allow / review / block
  decisions, confidence thresholds, and matched-term details.
- **Review desk** — a queue for borderline cases with allow/block decisions,
  notes, and the ability to change or undo a decision.
- **Insights** — live stats, check history, and per-tier breakdowns.
- **Operations** — a table browser with admin-only SQL queries and deletes, a
  terminal, and a file browser confined to `/app` (private keys and binaries are
  locked).
- **Multi-user access control** — Admin / Moderator / Viewer roles with fixed
  permissions enforced server-side on every route, HttpOnly sessions in
  PostgreSQL, 8-hour expiry, and failed-login lockout. Accounts are managed from
  Setup, with guards protecting the last enabled admin and the bootstrap
  admin's password.
- **Setup & service info** — account and configuration management, plus a live
  status/model/endpoints reference page.

## Repository layout

```
.
├── api/                 Go API, React console, certificates, Compose stack
│   ├── cmd/             Go commands (API server, certificate generator, migration tool)
│   ├── internal/        Go server packages (auth, store, config, httpx, model, worker, server)
│   │   └── store/migrations/  Embedded PostgreSQL schema migrations
│   ├── ui/              React + Vite management console (pages, components, lib, styles)
│   │   └── dist/        Built UI assets (served by the API)
│   ├── dist/            Built Go binary (profanity-api.exe)
│   ├── worker.py        Python ONNX inference worker
│   ├── Dockerfile       Multi-stage container image
│   ├── docker-compose.yml
│   ├── Taskfile.yml     Build and run tasks
│   └── README.md        Documentation index
├── docs/                Topic-based documentation (architecture, API, UI, database…)
├── v6/                  Trained model files (released separately)
├── old_models/          Archived model checkpoints
├── train_data/          Training data
└── Taskfile.yml         Root entry point — delegates to api/Taskfile.yml
```

## Quick start

```powershell
task              # list all tasks
task certs        # generate local TLS certificates (first run only)
task build        # build the UI and the Go binary
task up           # build and start the full Compose stack
```

The console is served at <https://localhost:8080/?v=6>. For frontend work,
`task dev` starts the Vite HTTPS dev server at <https://localhost:5173/?v=6>.

## Documentation

The full guide is in [`docs/`](docs/), indexed from [`api/README.md`](api/README.md):

- [Architecture](docs/architecture.md) and [Build and run](docs/build-and-run.md)
- [Docker Compose deployment](docs/deployment.md) and [Database](docs/database.md)
- [Management UI](docs/ui-guide.md), [Authentication](docs/authentication.md) and [Configuration](docs/configuration.md)
- [API reference](docs/api-reference.md), [Examples](docs/examples.md) and [Behavior notes](docs/moderation-behavior.md)

Model, dataset and dependency attribution is in [`CREDITS.md`](CREDITS.md).
