# Build and run

← [API documentation index](../api/README.md)

Commands on this page assume `api/` as the working directory unless stated otherwise.

## Build with Task

The build is automated with [Task](https://taskfile.dev) (v3). Taskfiles live in
`api/Taskfile.yml` and at the repository root (the root one delegates to
`api/`), so the same commands work from either directory:

```powershell
task              # list all tasks
task build        # build the UI assets (ui/dist) and the Go binary (dist/profanity-api.exe)
task check        # go vet + go test
task certs        # generate certs/ if missing
task pg           # start only the Compose Postgres (publishes 127.0.0.1:5432)
task run          # build, then run the API locally over HTTPS on :8080
task dev          # run the Vite HTTPS dev server on :5173
task up           # build the UI and start the full Compose stack
task down         # stop the stack (data volumes are kept)
task logs         # follow the API container logs
task clean        # remove dist/ (Go binary) and ui/dist
```

`task build` only rebuilds what changed: `ui/` is skipped when `ui/dist` is
newer than its sources, and the Go binary in `dist/` is skipped when
`cmd/**/*.go`, `internal/**/*.go`, `go.mod`, `go.sum`, or
`internal/store/migrations/*.sql` are unchanged. `task binary` and `task ui`
build each part on its own. Both outputs are build artifacts: the UI goes to
`ui/dist/`, the Go executable to `dist/profanity-api.exe`.

`task run` starts the local binary against the **same Postgres the Compose stack
uses**. It first runs `task pg` (bringing up `cert-init` + Postgres and publishing
`5432` on loopback only) and stops the containerized `api` service so port 8080
is free. Use `task run` **or** `task up`, not both — both bind 8080. The local
binary reads `settings.json` and the local `../v6` model, so `/health` reports
the local model path; `task up` reports `/app/v6` from inside the container.

Compose defaults to `podman compose` (there is no `docker` shim here). Override
it if needed:

```powershell
task COMPOSE="docker compose" up
# or
$env:COMPOSE = "docker compose"; task up
```

## Run

From this directory:

```powershell
go run ./cmd/gen-certs certs   # first run only: local CA + server certificates
# certs/*.key and settings.json are private — see .gitignore
go run ./cmd/api -tls-cert certs\api.crt -tls-key certs\api.key   # HTTPS on :8080
# or
.\dist\profanity-api.exe -python python -model ..\v6 -worker .\worker.py `
  -tls-cert certs\api.crt -tls-key certs\api.key
```

Without `-tls-cert`/`-tls-key` the API serves plain HTTP; with both it serves
HTTPS on the same `-addr`, forces `Secure` session cookies, and sends HSTS.
`worker.py` and the `v6` model directory are auto-detected relative to the
executable/current directory. Override anything explicitly:

```powershell
.\dist\profanity-api.exe -addr 127.0.0.1:9000 -python python -model ..\v6 -worker .\worker.py `
  -tls-cert certs\api.crt -tls-key certs\api.key
```

Flags:

| Flag       | Default  | Meaning                                      |
|------------|----------|----------------------------------------------|
| `-addr`    | `:8080`  | Listen address                               |
| `-python`  | `python` | Python executable with the ML packages       |
| `-model`   | auto     | Model directory (the one containing `model_int8.onnx`) |
| `-worker`  | auto     | Path to `worker.py`                          |
| `-db-url`  | `postgres://soglia:soglia@localhost:5432/soglia?sslmode=verify-full&sslrootcert=certs/ca.crt` | PostgreSQL connection URL |
| `-secure-cookie` | `false` | Add `Secure` to the session cookie (automatic with HTTPS) |
| `-tls-cert` | empty   | TLS certificate file; together with `-tls-key` enables HTTPS (`SOGLIA_TLS_CERT`) |
| `-tls-key`  | empty   | TLS private key file; together with `-tls-cert` enables HTTPS (`SOGLIA_TLS_KEY`) |
