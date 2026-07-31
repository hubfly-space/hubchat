-- Workspace creation happens before a workspace id exists, so the normal
-- workspace-scoped idempotency ledger cannot protect the onboarding retry.
-- Keep this small user-scoped ledger separate rather than weakening the FK on
-- idempotency_keys or inventing a synthetic workspace.
CREATE TABLE user_idempotency_keys (
    id                  text PRIMARY KEY,
    user_id             text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    key                 text        NOT NULL,
    endpoint            text        NOT NULL,
    request_fingerprint bytea       NOT NULL,
    response_status     integer,
    response_body       jsonb,
    created_at          timestamptz NOT NULL DEFAULT now(),
    expires_at          timestamptz NOT NULL
);

CREATE UNIQUE INDEX user_idempotency_keys_lookup
    ON user_idempotency_keys (user_id, endpoint, key);

CREATE INDEX user_idempotency_keys_expiry
    ON user_idempotency_keys (expires_at);
