-- 0014_tasks.sql
--
-- Deterministic work items created by automation rules. Tasks are deliberately
-- small and explicit: they are not a second conversation system, only a
-- durable reminder linked to the record that caused it.

BEGIN;

CREATE TABLE tasks (
    id             text PRIMARY KEY,
    workspace_id   text NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    title          text NOT NULL,
    description    text NOT NULL DEFAULT '',
    state          text NOT NULL DEFAULT 'open',
    subject_type   text,
    subject_id     text,
    assignee_id    text REFERENCES workspace_members (id) ON DELETE SET NULL,
    due_at         timestamptz,
    created_by     text REFERENCES workspace_members (id) ON DELETE SET NULL,
    completed_at   timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tasks_state CHECK (state IN ('open', 'completed', 'cancelled')),
    CONSTRAINT tasks_subject_type CHECK (subject_type IS NULL OR subject_type IN ('conversation', 'ticket', 'customer', 'feedback'))
);

CREATE INDEX tasks_by_workspace_state
    ON tasks (workspace_id, state, due_at NULLS LAST, created_at DESC);

CREATE INDEX tasks_by_subject
    ON tasks (workspace_id, subject_type, subject_id)
    WHERE subject_id IS NOT NULL;

COMMIT;
