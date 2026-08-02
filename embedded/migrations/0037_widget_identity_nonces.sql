-- 0037_widget_identity_nonces.sql
--
-- Signed customer identity tokens carry a nonce and are single-use. Keeping
-- the consumed nonce in PostgreSQL makes that guarantee hold across HTTP
-- processes and restarts, rather than only inside one widget process.

BEGIN;

CREATE TABLE widget_identity_nonces (
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    nonce_hash   bytea       NOT NULL,
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, nonce_hash)
);

CREATE INDEX widget_identity_nonces_expiry
    ON widget_identity_nonces (expires_at);

COMMIT;
