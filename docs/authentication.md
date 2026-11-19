# Authentication

← [API documentation index](../api/README.md)

Commands on this page assume `api/` as the working directory unless stated otherwise.

The UI and the moderation APIs require a signed-in account. Multiple accounts
are supported with three fixed roles (admin, moderator, viewer); see the
[role table](ui-guide.md#roles-and-permissions). The bootstrap admin comes from
the environment before the first start:

```powershell
$env:SOGLIA_USER = 'admin'
$env:SOGLIA_PASSWORD = 'choose-a-strong-password'
.\dist\profanity-api.exe -python python -model ..\v6 -worker .\worker.py
```

If `SOGLIA_PASSWORD` is not set, the API generates a random 14-character
password once and stores its salted hash in the database. It prints the password
on first run; later restarts reuse it:

```
INFO no password configured, generated one user=admin password=xxxxxxxxxxxxxx
INFO using account stored in database user=admin
```

That bootstrap account is an admin. On upgrade, existing accounts are migrated
to admin and enabled. Add moderator and viewer accounts from the **Setup → Users
& permissions** panel (or the [`/users` API](api-reference.md#user-management-users-admin-only)) — only admins can manage accounts.

Sign in on the UI login screen with any enabled account. The server sets an
HttpOnly, `Secure` cookie named `soglia_session` valid for 8 hours (the `Secure`
flag is forced on when HTTPS is enabled). Sessions are stored in Postgres over
TLS, so you stay signed in across API restarts and are resolved per request —
disabling an account or changing its role takes effect immediately. Five failed
logins from the same IP cause a 5-minute lockout. Each user can change their own
username/password from the **Setup** page; changing a password invalidates only
that account's sessions.

Public endpoints: `/` (the console), `/ui/` (legacy alias), `/info` (service
description), `/health`, `/auth/login`, `/auth/logout`, `/auth/me`.

Protected, any signed-in account: `/config`, `/stats`, `/history` (GET),
`/moderate`, `/moderate/batch`.

Protected, `moderate` permission (admin, moderator): `DELETE /history`.

Protected, `review` permission (admin, moderator): `/reviews`.

Protected, admin only: `/db`, `/db/table`, `/db/query`, `/files`, `/terminal`,
`/terminal/cancel`, `/setup`, `/users`.
