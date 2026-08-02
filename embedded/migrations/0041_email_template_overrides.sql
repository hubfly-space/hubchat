-- 0041_email_template_overrides.sql
-- Workspace-owned plain-text customer email template overrides.

BEGIN;

CREATE TABLE email_template_overrides (
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    key text NOT NULL,
    subject text NOT NULL,
    body text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    updated_by text REFERENCES workspace_members(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, key)
);

CREATE INDEX email_template_overrides_updated
    ON email_template_overrides (workspace_id, updated_at DESC);

COMMIT;
