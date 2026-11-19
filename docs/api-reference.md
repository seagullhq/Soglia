# API reference

← [API documentation index](../api/README.md)

For request examples, see [Examples](examples.md). For how moderation decisions
are computed, see [Behavior notes](moderation-behavior.md).

### `POST /auth/login`

Request: `{"username": "admin", "password": "..."}`. On success returns
`{"user":"admin","role":"admin","permissions":["moderate","review",...]}` and
sets the `soglia_session` cookie. Disabled accounts are rejected.

### `POST /auth/logout`

Invalidates the current session and clears the cookie.

### `GET /auth/me`

Returns `{"user":"admin","role":"admin","permissions":[...]}` for a valid
session, otherwise `401`. The role and permissions come from the account in
Postgres on every call.

### `GET /health`

```json
{"status":"ok","model":"C:\\Users\\DANIELE\\Desktop\\profanity_filter-fc\\v6"}
```

### `POST /moderate`

Request: `{"text": "some message"}`

Response:

```json
{"action":"block","tier":"Tier 1","prob":1,"ms":0.155,"matched":"vaffanculo"}
```

A Tier 2 block/review also carries `matched`:

```json
{"action":"block","tier":"Tier 2","prob":0.995,"ms":2.8,"matched":"f**king"}
```

| Field     | Meaning                                                                 |
|-----------|-------------------------------------------------------------------------|
| `action`  | `allow`, `review`, or `block`                                           |
| `tier`    | `FastPath` (empty/very short), `Tier 1` (blocklist), `Tier 2` (ONNX model) |
| `prob`    | Toxicity probability after temperature scaling (1.0 for Tier 1)         |
| `ms`      | Processing time in milliseconds                                         |
| `matched` | Offending word. Tier 1: exact normalized blocklist token. Tier 2: attributed word (see [Behavior notes](moderation-behavior.md)). Present on `block`/`review`, omitted on `allow`. |

### `POST /moderate/batch`

Request: `{"texts": ["message one", "message two"]}` (1–100 items)

Response: `{"results": [ ...same objects as /moderate... ]}`

### `GET /config`

Returns the calibration thresholds, blocklist size, tokenizer limit, the model
spec read from `v6/config.json` (architecture, task, layers, heads, hidden size,
vocabulary, transformers version) and the sizes of the model files. This is what
the management UI uses for its sidebar.

### `GET /stats`

Counters computed from the database: `total`, `allow`, `review`,
`pending_reviews` (review checks with no decision yet), `block`, `tier1`,
`tier2`, `fastpath`, and `avg_ms`.

### `GET /history`

Returns the latest checks from the database, newest first. Default limit is 12
(`?limit=50` for more, maximum 100). Each entry has `id`, `text`, `action`,
`tier`, `prob`, `ms`, optional `matched`, and `at` (Unix milliseconds).

### `DELETE /history`

Clears ordinary checks; the stats counters for those checks reset to zero.
Review-band checks and their decisions are kept so the moderator queue survives.
Requires the `moderate` permission (admin or moderator).

### `GET /reviews`

Returns the moderator review queue as `{ "items": [...], "pending": 3 }`.
Pending checks come first, then newest first, up to 100. Each item has the same
fields as a history entry plus `decision`, `note`, `reviewer` and `decided_at`
when a moderator has decided it. Requires the `review` permission (admin or
moderator).

### `POST /reviews`

Records or updates a moderator decision for a review check:

```json
{"id": 42, "decision": "allow", "note": "idiom, not an insult"}
```

`id` must be the numeric check id from `GET /reviews` or `GET /history`, and
`decision` must be `allow` or `block`. `note` is optional (up to 2,000
characters). Returns `{ "ok": true }`, or 404 if the check does not exist or was
not placed in the review band. The acting username is recorded as `reviewer`.
Calling it again for the same id **changes** the decision, which is how a
mistaken approval is corrected. Requires the `review` permission.

### `DELETE /reviews?id=42`

Reopens a decided item by deleting only its decision; the check stays in the
review queue as pending. Returns `{ "ok": true }`, or 404 if there is no decision
for that id. The Review tab exposes this as **Undo decision**, alongside
**Change to Block/Allow** above. Requires the `review` permission.

### `GET /db`

Lists public tables with row counts and columns:
`{ "tables": [{ "name": "checks", "rows": 12, "columns": [...] }] }`.

### `GET /db/table?name=checks`

Returns up to 200 rows from one table as
`{ "columns": [...], "rows": [...], "truncated": false }`. Credential columns
are masked. Requires a session.

### `POST /db/query`

Runs one statement, e.g. `{"sql": "SELECT action, count(*) FROM checks GROUP BY action"}`.
The whole `/db` family requires the `db` permission (admin only). `SELECT` and
`WITH` run in a read-only transaction with a 5-second timeout, returning the
same shape as `GET /db/table`. `DELETE` (including `WITH ... DELETE`) is allowed
for admins and returns `{ "deleted": 3 }` with the number of affected rows.
Other writes and multi-statement input are rejected with 400; SQL errors also
return 400.

### `GET /setup`

Returns the current account and service settings (protected):

```json
{
  "account": {"username": "admin", "role": "admin"},
  "settings": {
    "database_url": "postgres://soglia:********@localhost:5432/soglia?sslmode=verify-full&sslrootcert=certs/ca.crt",
    "model_dir": "C:\\...\\v6",
    "python": "python",
    "worker": "C:\\...\\worker.py",
    "secure_cookie": true
  },
  "status": {
    "settings_file": "settings.json",
    "database": "connected",
    "model_dir_exists": true,
    "worker_exists": true,
    "restart_required": false
  }
}
```

The database password is masked as `********`.

### `PUT /setup`

Accepts any subset of `username`, `password`, `database_url`, `model_dir`,
`python`, `worker`, and `secure_cookie` and returns
`{"ok":true,"settings_changed":...,"account_changed":...}`. Account changes
apply immediately (setting a password signs the user out); service settings are
written to `settings.json` and require an API restart. Submitting the
`********` mask as `database_url` keeps the existing password.

### User management (`/users`, admin only)

All four methods require the `users` permission (admins).

`GET /users` lists accounts:

```json
{"users":[{"username":"admin","role":"admin","enabled":true,"created_at":1700000000000}]}
```

`POST /users` creates one:

```json
{"username":"mod","password":"a-strong-password","role":"moderator"}
```

`role` must be `admin`, `moderator` or `viewer`; the password must be at least 8
characters. A duplicate username returns `409`.

`PUT /users` changes an account — send `username` plus any of `role`, `enabled`
and `password`:

```json
{"username":"mod","role":"viewer","enabled":true,"password":"new-password"}
```

Setting `password` invalidates that account's sessions. The main admin
(bootstrap account, id 1) cannot have its password reset this way (400); use
`SOGLIA_PASSWORD` and a restart. The only enabled admin cannot be demoted or
disabled (400).

`DELETE /users?username=mod` removes an account and its sessions. You cannot
remove your own account, and the only enabled admin cannot be removed (400).
Successful calls return `{"ok":true}`.

### `GET /files`

Protected file browser used by the UI's **Files** tab. Query parameter `path`
must resolve inside `/app`; an empty path means `/app`. Symlinks are resolved
before the check, so links out of `/app` are rejected with 400. The parent of
`/app` is reported as empty, so the UI cannot navigate above the root. A
directory returns:

```json
{"path":"/app","parent":"","is_dir":true,
 "entries":[{"name":"v6","path":"/app/v6","is_dir":true,"size":4096,
             "mod_time":"2026-11-18 22:10","locked":false}]}
```

A file returns `{..., "is_dir":false, "file":{...}}` with `name`, `path`, `size`,
`content`, `truncated`, `locked`, `reason`, and `binary`.

Safeguards, because this endpoint can read anything the API process can:

- It requires the `files` permission (admin only) and is confined to `/app`.
- Contents are never returned for private keys (`*.key`, `*.pem`, `id_rsa`, …),
  `.env` files, credential stores (`.netrc`, `.npmrc`, `.htpasswd`, `credentials`,
  `passwd`, `shadow`) and known binary formats. Those files are still listed,
  marked `locked: true` with a `reason`.
- `settings.json` and `.env.example` are readable but passwords, tokens and
  secrets are replaced with `********`.
- Text previews are capped at 256 KB (`truncated: true`); files containing NUL
  bytes or invalid UTF-8 are reported as binary with no content.

### `POST /terminal`

**Disabled by default.** The routes exist only when the API starts with
`-terminal` or `SOGLIA_TERMINAL_ENABLED=true`; otherwise they return 404. They
also require the `terminal` permission (admin only) and a valid session. This
is a read-only diagnostics endpoint, not a shell: it cannot modify the
container or run arbitrary programs.

```json
{"id":"client-generated-id","command":"ls -la","cwd":"/app"}
```

Returns `{ "output": "...", "cwd": "/app", "code": 0 }`. `cwd` is optional and
defaults to `/app`. `cd` is handled by the server without starting a process;
pass the returned `cwd` with the next command to persist it. `id` is optional
but should be unique per command so it can be cancelled.

Safeguards:

- **No shell.** The command is tokenised, not interpreted. Pipes, redirects,
  `;`, `&`, `$()`, backticks, newlines and other shell operators are rejected.
  Quotes only group words.
- **Allow-list.** Only trusted, read-only system utilities are accepted
  (`ls`, `cat`, `grep`, `find`, `ps`, `df`, `du`, `stat`, `head`, `tail`, …),
  resolved from `/usr/bin` or `/bin` and rejected if writable by other users.
  Interpreters, editors, package managers, network tools and Docker/Podman are
  never available. The endpoint is supported inside the Linux container only.
- **Read-only arguments.** Options that can execute or write (`find -exec`,
  `-delete`, `-fprint`, `tail -f`, …) are rejected, as are locked files
  (keys, `.env`, credential stores). File arguments are resolved with symlinks
  and must stay inside `/app`.
- **No inherited secrets.** Child processes get a minimal environment
  (`PATH`, locale, `TERM`, `HOME`); known secret values are also scrubbed from
  command output and audit records.
- **Bounded.** 8 KB command, 15 s timeout, and 64 KB of output captured while
  it streams (the command is killed as soon as the cap is hit). One command per
  session at a time and 30 requests/minute/session.

Every accepted or rejected command is audit-logged with the user, a hashed
session identifier, client IP, command, resolved program and arguments, working
directory, exit code, output size, duration and outcome.

### `POST /terminal/cancel`

```json
{"id":"client-generated-id"}
```

Returns `{ "cancelled": true }` only when a command with that id is running in
the **same session**, otherwise `{ "cancelled": false }` (the Stop button stays
idempotent). Cancelling kills the command's whole process group; the
`/terminal` request then returns with `code: 130` and output ending in
`command cancelled`.
