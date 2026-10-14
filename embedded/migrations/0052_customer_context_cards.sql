CREATE TABLE customer_context_cards (
 id text PRIMARY KEY,
 workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 title text NOT NULL,
 fields jsonb NOT NULL DEFAULT '[]',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX customer_context_cards_workspace ON customer_context_cards(workspace_id,created_at,id);
