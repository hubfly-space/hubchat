-- 0031_trusted_devices.sql
-- Revocable, hashed browser credentials issued only after a successful second
-- factor. A trusted device never replaces the session cookie or grants API
-- access by itself; it only lets the next sign-in skip the TOTP challenge.

BEGIN;

CREATE TABLE trusted_devices (
    id           text PRIMARY KEY,
    user_id      text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL UNIQUE,
    name         text        NOT NULL DEFAULT '',
    user_agent   text,
    ip           inet,
    expires_at   timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX trusted_devices_by_user ON trusted_devices (user_id, created_at DESC)
    WHERE revoked_at IS NULL;

COMMIT;
