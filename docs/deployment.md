# Docker Compose deployment

← [API documentation index](../api/README.md)

Commands on this page assume `api/` as the working directory unless stated otherwise.

The whole stack — TLS-enabled Postgres plus the HTTPS API — can run in
containers. From `api/`:

```powershell
go run ./cmd/gen-certs certs          # once, if certs/ is missing
Copy-Item .env.example .env           # then set a strong SOGLIA_PASSWORD
docker compose up -d --build          # or: podman compose up -d --build
```

Open `https://localhost:8080/ui/?v=6`. A local `.env` already exists with the
current credentials. Stop the local API/Postgres first — Compose publishes
`8080` and uses the same certs.

The stack has three services:

- `cert-init` copies `certs/` into a named volume with `postgres`-owned `0600`
  keys, then exits. It runs before Postgres.
- `soglia-postgres` listens inside the compose network as
  `soglia-postgres:5432` with `ssl=on` and a healthcheck. It also publishes
  `127.0.0.1:5432` so a local `task run` binary can use the same database over
  TLS; the port is bound to loopback only, never exposed on the LAN.
- `api` connects with `sslmode=verify-full` against `certs/ca.crt` and serves
  HTTPS with Secure cookies. The `v6` model (~470 MB) is baked into the image,
  so rebuild after changing the model.

Data lives in the `pg-data` volume and Setup edits in `api-settings`; both
survive `docker compose down`.

```powershell
docker compose ps
docker compose logs -f api
docker compose down        # stop, keep data
docker compose down -v     # stop and delete data/cert volumes
```
