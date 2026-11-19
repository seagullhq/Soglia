# Service settings file

← [API documentation index](../api/README.md)

`api/settings.json` holds the UI-editable service configuration
(`database_url`, `model_dir`, `python`, `worker`, `secure_cookie`). It is created
automatically on first start with the current values. On startup, explicit
command-line flags and environment variables still take precedence over the
file:

- `-db-url` / `SOGLIA_DATABASE_URL` / `DATABASE_URL`
- `-model`, `-python`, `-worker`, `-secure-cookie`

Keep this file private — it contains the database password in plaintext.
