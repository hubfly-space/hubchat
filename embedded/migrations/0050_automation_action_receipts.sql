CREATE TABLE automation_action_receipts (
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    action_key text NOT NULL,
    completed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, action_key)
);
