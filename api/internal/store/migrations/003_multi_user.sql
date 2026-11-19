-- Multiple console accounts with fixed roles. The original single-account
-- check is dropped and id gets a sequence so Setup can create more users.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_id_check;

CREATE SEQUENCE IF NOT EXISTS users_id_seq;
ALTER TABLE users ALTER COLUMN id SET DEFAULT nextval('users_id_seq');
SELECT setval('users_id_seq', GREATEST(COALESCE((SELECT MAX(id) FROM users), 1), 1));

ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'admin';
ALTER TABLE users ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'moderator', 'viewer'));
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE (username);
