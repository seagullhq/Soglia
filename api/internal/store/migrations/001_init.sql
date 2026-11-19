-- Initial console schema: account, sessions, and moderation history.

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    username TEXT NOT NULL,
    salt TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    expires_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS checks (
    id BIGSERIAL PRIMARY KEY,
    text TEXT NOT NULL,
    action TEXT NOT NULL,
    tier TEXT NOT NULL,
    prob DOUBLE PRECISION NOT NULL,
    ms DOUBLE PRECISION NOT NULL,
    matched TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_checks_created ON checks(created_at DESC);
