-- 0035_portal_sso_nonces.sql
-- Signed portal SSO tokens carry a nonce.  Keeping the nonce durable makes
-- each asserted login one-time-use and closes replay within the token TTL.

BEGIN;

CREATE TABLE portal_sso_nonces (
    portal_id    text        NOT NULL REFERENCES portals (id) ON DELETE CASCADE,
    workspace_id text        NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    nonce        text        NOT NULL,
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz NOT NULL DEFAULT now(),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (portal_id, nonce)
);

CREATE INDEX portal_sso_nonces_expiry
    ON portal_sso_nonces (expires_at);

COMMIT;
