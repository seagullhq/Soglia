# Management UI

← [API documentation index](../api/README.md)

Commands on this page assume `api/` as the working directory unless stated otherwise.

The React console lives in `ui/` and is served at **https://localhost:8080/ui/**
when `ui/dist` exists (the built assets are included here). It shows service
state, model architecture and file sizes, the decision path and thresholds, a
single-message checker with a probability scale, a batch checker, and the last
12 checks. The main console has four tabs: **Moderation** (the checker, batch
checker and recent checks together), **Review** (the moderator workspace),
**Files** and **Database**. Tabs and controls are shown according to the signed-in
user's role, and every API route enforces the same permissions server-side —
hiding a button is not the only safeguard.

## Roles and permissions

| Role | Permissions |
|------|-------------|
| **Admin** | Checks, review decisions, Files, Terminal, Database, Setup and user management |
| **Moderator** | Checks and review decisions |
| **Viewer** | Read-only: service state, model details, stats and **Recent checks** |

A viewer sees only the **Moderation** tab with the checker, batch checker and
**Clear history** hidden — **Recent checks** remains readable. Moderators also
get the **Review** tab. **Files**, **Database**, **Terminal**, **Setup** and user
management are admin-only. Permissions are resolved from the account in Postgres
on every request, so role or enabled changes take effect without a restart or
re-login.

The **Database** tab browses the Postgres database. The left pane lists tables,
row counts and columns; choosing one runs `SELECT * FROM <table>` in the query
pane. You can also type your own `SELECT` / `WITH` statement and press Ctrl +
Enter. Results are capped at 200 rows, reads run inside a read-only transaction,
and credential columns (`password`, `password_hash`, `salt`, `token_hash`) are
masked as `********`. Signed-in admins can also run `DELETE` statements: the UI
asks for confirmation, shows the deleted-row count, and refreshes table counts,
stats, reviews and history. Other writes (`INSERT`, `UPDATE`, `DROP`, etc.) and
multi-statement input are rejected. Open it directly with
`https://localhost:8080/ui/?v=6&tab=db`.

Messages the model places in the review band appear in the dedicated **Review**
tab, laid out as a compact queue on the left and a detail pane on the right.
Selecting an item shows its full text, probability scale, matched word, tier and
timestamp; a moderator can add an optional note and choose **Allow message** or
**Block message**. The **All / Pending / Decided** filter narrows the list;
**All** is the default, and deciding an item keeps it in view with its decision
badge instead of removing it. Decided items retain their reviewer, time and
note. A mistaken decision can be corrected with **Change to Block/Allow** or
removed entirely with **Undo decision**, which returns the item to pending — no
SQL needed. **Open in checker** loads the message back into the Moderation tab.
The pending count appears on the tab itself and in the **Awaiting review** stat
card. The selected tab is written to the URL, so a reload returns
to the Review workspace. Queue items and decisions are stored in Postgres and
survive restarts and **Clear history**. Open it directly with
`https://localhost:8080/ui/?v=6&tab=reviews`.

The **Files** tab browses `/app` on the machine running the API and previews
text files. Enter a path under `/app`, use the quick jumps (`/app` / model), or
click through folders; navigating above `/app` is blocked by the server. Locked
entries (private keys, binaries) show a reason on a second line while their
size stays on the first line, even in narrow windows. Clicking a file in the sidebar's **Model files** list opens it
directly in this tab. Open it with
`https://localhost:8080/ui/?v=6&tab=files` (the older `view=files` link also
works). See [`GET /files`](api-reference.md#get-files) for the safeguards applied
to file contents.

The **Terminal** button in the top bar opens a fixed-size modal shell. Commands
run on the machine hosting the API as the API process user; `cd` persists
between commands and ↑ / ↓ walks command history (kept across modal reopens and
page reloads). `clear` / `cls` or Ctrl+L empties the output, Ctrl+C stops a
running command (or clears the input line), **Copy** copies the whole transcript,
and Esc or a backdrop click closes the modal. The **Stop** button replaces **Run**
while a command is executing and interrupts the command and its child processes;
long-running commands are also capped at 15 seconds. This is full shell access
for signed-in operators — it is not restricted by the file browser's secret
masking, so treat the session credential as an administrator credential.

For development with hot reload:

```powershell
# terminal 1 — Go API (HTTPS) on :8080
go run ./cmd/api -tls-cert certs\api.crt -tls-key certs\api.key

# terminal 2 — Vite (HTTPS) on :5173, proxying API calls to :8080
cd ui
npm install
npm run dev
# open https://localhost:5173/ui/
```

After UI changes, rebuild the assets so the Go process serves them:

```powershell
cd ui
npm run build
```

After changes to the Go sources, restart the API as well — a running process
keeps serving the old binary. If the console looks stale after a rebuild, open the
versioned URL (`https://localhost:8080/ui/?v=6` or
`https://localhost:5173/ui/?v=6`), which bypasses any cached HTML. The sidebar
shows the current `build N` marker so you can confirm which bundle loaded. The
UI also shows a readable error instead of a blank page if rendering fails.

Because the certificate is issued by a local CA, your browser will warn until
you trust `certs/ca.crt` (Windows: `certutil -addstore -user Root certs\ca.crt`).
On Windows, curl needs `--ssl-no-revoke` with a local CA that has no CRL:

```powershell
curl.exe --ssl-no-revoke --cacert certs\ca.crt https://localhost:8080/health
```

The console is called **Soglia** ("threshold"), matching the two decision
thresholds the model uses.

## Setup page

The **Setup** screen (top-right button, sidebar link, or the `?view=setup` deep
link, e.g. `https://localhost:8080/ui/?v=6&view=setup`) edits both the console
account and the service configuration:

- **Account** — rename the current user and/or set a new password. Changes apply
  immediately; renaming keeps you signed in, while setting a password
  invalidates sessions and signs you out.
- **Users & permissions** (admin-only) — list all accounts with their role and
  enabled state. Add an account with a username, password and role; change a
  role, enable/disable an account, reset its password, or remove it. The only
  enabled admin cannot be demoted, disabled or removed, and you cannot delete
  your own account. The **main admin** (the bootstrap account, id 1) is marked
  in the list; its password is owned by `SOGLIA_PASSWORD` and cannot be reset
  from this panel — set the env var and restart instead.
- **Database / Service** — edit the PostgreSQL URL, Python executable, worker
  path, model directory, and secure-cookie flag. These are written to
  `settings.json` and take effect after the API is restarted (the page shows a
  "Restart needed" status until then).

The database password is masked as `********`; submitting it unchanged keeps the
stored password. Saving only service settings does not sign you out.
