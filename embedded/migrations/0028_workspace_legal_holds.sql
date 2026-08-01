-- 0028_workspace_legal_holds.sql
-- Workspace-scoped retention overrides for active investigations and legal matters.

BEGIN;

CREATE TABLE workspace_legal_holds (
    id                text PRIMARY KEY,
    workspace_id      text NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    category          text NOT NULL CHECK (category IN ('all', 'events', 'sessions', 'webhooks', 'surveys', 'audit')),
    reason            text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
    created_by        text REFERENCES workspace_members (id) ON DELETE SET NULL,
    released_by       text REFERENCES workspace_members (id) ON DELETE SET NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    released_at       timestamptz
);

CREATE INDEX workspace_legal_holds_active
    ON workspace_legal_holds (workspace_id, category, created_at DESC, id)
    WHERE released_at IS NULL;

CREATE INDEX workspace_legal_holds_history
    ON workspace_legal_holds (workspace_id, created_at DESC, id);

COMMIT;
