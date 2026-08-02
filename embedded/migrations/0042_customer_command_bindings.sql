-- 0042_customer_command_bindings.sql
-- Explicit, host-owned commands that an agent may deliver to a visitor.

BEGIN;

CREATE TABLE customer_command_bindings (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    created_by text REFERENCES workspace_members(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE INDEX customer_command_bindings_workspace ON customer_command_bindings(workspace_id, created_at DESC, id DESC);

CREATE TABLE customer_command_invocations (
    id text PRIMARY KEY,
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    binding_id text NOT NULL REFERENCES customer_command_bindings(id) ON DELETE RESTRICT,
    conversation_id text NOT NULL,
    visitor_id text NOT NULL,
    member_id text NOT NULL REFERENCES workspace_members(id) ON DELETE RESTRICT,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','delivered','acknowledged','ignored','failed','expired')),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    delivered_at timestamptz,
    acknowledged_at timestamptz,
    error text
);

CREATE INDEX customer_command_invocations_conversation ON customer_command_invocations(workspace_id, conversation_id, created_at DESC);
CREATE INDEX customer_command_invocations_expiry ON customer_command_invocations(status, expires_at);

COMMIT;
