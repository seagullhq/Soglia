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

## Dokploy

Use the compose file at the repository root, `docker-compose.yml`. Dokploy looks
for `./docker-compose.yml` by default; `api/docker-compose.yml` is the local
stack and expects generated certificates plus a local `.env`, so it is not
usable from a fresh clone.

The root compose file differs in three ways:

- `api/Dockerfile.dokploy` builds the UI from source (no committed `ui/dist`)
  and does not copy `v6` into the image (the model is mounted at runtime).
- The API serves plain HTTP on `:8080`; Traefik terminates public TLS and
  issues the Let's Encrypt certificate. `-secure-cookie` is passed on the
  command line because the app cannot see the proxy's TLS connection.
- PostgreSQL runs inside the Compose project with `sslmode=disable` on the
  internal Docker network. Use `SOGLIA_DATABASE_URL` from Dokploy's environment
  instead if you prefer a managed database.

Before the first deploy, create the model directory on the host and copy the
`v6` files there (`model_int8.onnx`, `tokenizer.json`, `calibration.json`,
`clean_blocklist.txt` are required; `config.json` and the larger files are
optional but feed the UI's file sizes):

Dokploy cleans up absolute host bind mounts on each deploy, so the model uses
`../files/v6` relative to the compose file, i.e. the `files` directory of the
Dokploy service (for a service cloned into
`/etc/dokploy/compose/<service>/code`, upload to
`/etc/dokploy/compose/<service>/files/v6/`):

```bash
mkdir -p /etc/dokploy/compose/<service>/files/v6
# rsync -avP /path/to/v6/ root@SERVER:/etc/dokploy/compose/<service>/files/v6/
chmod -R a+rX /etc/dokploy/compose/<service>/files/v6
```

In Dokploy: create a **Compose** service pointing at the Git repo, leave the
compose path as `./docker-compose.yml`, then open the **Environment** tab and
save the variables as plain lines (they are written to `.env`; compose
interpolation reads that file):

```
SOGLIA_USER=admin
SOGLIA_PASSWORD=choose-a-strong-password
POSTGRES_PASSWORD=urlsafe123
```

Environment variables are not injected into containers automatically — the
compose file already references each one with `${NAME}`, so saving them here
and redeploying is enough. Add the domain on port `8080` with HTTPS enabled.
Settings edited from the Setup page persist in the `soglia-settings` volume.
Keep `POSTGRES_PASSWORD` URL-safe (letters and digits) because it is
interpolated into the connection URL.
