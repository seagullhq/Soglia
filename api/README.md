# Profanity Filter API (Go) + management UI

This is the documentation index. The full guide is split by topic under
[`docs/`](../docs/) at the repository root.

> **Model availability:** the trained `v6` model is not included in this
> repository and will be released later. To run the service, place the model at
> the configured `model_dir` (`/app/v6` in the container, `../v6` for local
> development).

Commands in the topic pages assume `api/` as the working directory unless stated
otherwise.

## Documentation

| Page | What it covers |
|------|----------------|
| [Architecture](../docs/architecture.md) | Request flow, Go package layout, TLS links, worker process, dependencies |
| [Build and run](../docs/build-and-run.md) | Task targets, incremental builds, CLI flags, local HTTPS startup |
| [Docker Compose deployment](../docs/deployment.md) | The three-container stack, volumes, start/stop commands |
| [Management UI](../docs/ui-guide.md) | Tabs, roles and permissions, Setup page, hot reload, browser trust |
| [Authentication](../docs/authentication.md) | Bootstrap admin, sessions, lockout, route permissions |
| [Database](../docs/database.md) | What is stored, local TLS Postgres, schema migrations, SQLite import |
| [API reference](../docs/api-reference.md) | Every endpoint with requests, responses and required permissions |
| [Examples](../docs/examples.md) | PowerShell and curl login/moderation examples |
| [Configuration](../docs/configuration.md) | `settings.json`, precedence of flags and environment variables |
| [Behavior notes](../docs/moderation-behavior.md) | Tier 1 / Tier 2 logic, `matched` attribution, normalization, CORS |

Model, dataset and dependency attribution is in [`CREDITS.md`](../CREDITS.md).
