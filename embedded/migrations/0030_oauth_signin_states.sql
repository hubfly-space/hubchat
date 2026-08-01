-- 0030_oauth_signin_states.sql
-- Single-use, short-lived state for the deployment-level OAuth/OIDC adapter.

BEGIN;

CREATE TABLE oauth_signin_states (
    id           text PRIMARY KEY,
    state_hash   bytea       NOT NULL UNIQUE,
    provider     text        NOT NULL,
    redirect_to  text        NOT NULL DEFAULT '',
    expires_at   timestamptz NOT NULL,
    consumed_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_signin_states_expiry ON oauth_signin_states (expires_at)
    WHERE consumed_at IS NULL;

COMMIT;
