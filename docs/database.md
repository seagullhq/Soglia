# Database

← [API documentation index](../api/README.md)

Commands on this page assume `api/` as the working directory unless stated otherwise.

The API stores its data in PostgreSQL. The connection comes from
`SOGLIA_DATABASE_URL`, then `DATABASE_URL`, then the `-db-url` flag (which
defaults to the local Podman database below). Schema migrations are applied
automatically on startup. It stores:

- console accounts with a role (`admin`, `moderator`, `viewer`) and an enabled
  flag (salted SHA-256 hash, no plaintext password);
- login sessions, so a restart no longer signs you out;
- every moderation check, which powers **Recent checks**, **Review queue** and
  the **stats** strip in the UI;
- moderator decisions (allow/block, note, reviewer, time) for review checks.

The most recent 5,000 checks are kept. Both history and stats survive restarts.

Local Postgres via Podman, with TLS enabled. The server key must be owned by
`postgres` (uid 70) with mode `0600`, so the generated certs are copied into a
dedicated volume with the right ownership:

```powershell
# 1. Data volume (preserved across container replacement)
podman volume create soglia-pgdata
# 2. Cert volume, owned by the postgres user
podman volume rm soglia-certs -f 2>$null
podman volume create soglia-certs
$certs = (Resolve-Path '.\certs').Path
podman run --rm --entrypoint /bin/sh -v soglia-certs:/certs -v "${certs}:/src:ro" `
  docker.io/library/postgres:16-alpine `
  -c 'cp /src/*.crt /src/*.key /certs/ && chown -R 70:0 /certs && chmod 600 /certs/*.key && chmod 644 /certs/*.crt'
# 3. Postgres with SSL on
podman run -d --name soglia-postgres --replace `
  -v soglia-pgdata:/var/lib/postgresql/data -v soglia-certs:/certs:ro `
  -e POSTGRES_USER=soglia -e POSTGRES_PASSWORD=soglia -e POSTGRES_DB=soglia `
  -p 5432:5432 docker.io/library/postgres:16-alpine `
  -c ssl=on -c ssl_cert_file=/certs/postgres.crt `
  -c ssl_key_file=/certs/postgres.key -c ssl_ca_file=/certs/ca.crt
```

The connection string uses `sslmode=verify-full` with `sslrootcert=certs/ca.crt`,
so both encryption and server identity are verified. Check with:

```powershell
podman exec soglia-postgres psql -U soglia -d soglia -tAc 'show ssl;'   # on
podman exec soglia-postgres psql -U soglia -d soglia -tAc `
  "select usename, ssl, version from pg_stat_ssl join pg_stat_activity using (pid);"
```

The container is already running here with that configuration. The database
volume `soglia-pgdata` survives container replacement. If you regenerate the
certificates, repeat step 2 and recreate the container. To reset everything,
drop the database (or remove the volume) and restart the API — the next start
generates and prints a new password.

## Schema migrations

SQL files in `api/internal/store/migrations/` are embedded in the Go binary and applied in
filename order on startup. Each file runs in one transaction and is recorded in
`schema_migrations`, so restarting is safe and new files are picked up
automatically. To add a change, create `002_something.sql` (or the next number)
and rebuild.

The one-time data move from the old SQLite database is a repeatable command:

```powershell
# Schema migrations run first — start the API once, or use an initialized DB.
go run ./cmd/migrate-sqlite -sqlite soglia.sqlite.backup
```

It imports the account and check history with `ON CONFLICT DO NOTHING`, so it can
be run more than once. Sessions are deliberately not copied.
