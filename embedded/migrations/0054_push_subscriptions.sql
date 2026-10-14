CREATE TABLE push_subscriptions (
 id text PRIMARY KEY,
 workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 member_id text NOT NULL REFERENCES workspace_members(id) ON DELETE CASCADE,
 endpoint_hash text NOT NULL,
 subscription bytea NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,member_id,endpoint_hash)
);
