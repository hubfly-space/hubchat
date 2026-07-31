-- 0018_customer_notification_preferences.sql
-- Portal customers control the non-essential messages they receive. Replies
-- remain transactional and cannot be disabled here; these preferences cover
-- ticket status, feedback, changelog, and survey delivery.

CREATE TABLE customer_notification_preferences (
    customer_id       text PRIMARY KEY REFERENCES customers (id) ON DELETE CASCADE,
    workspace_id      text NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    ticket_status     boolean NOT NULL DEFAULT true,
    feedback_updates  boolean NOT NULL DEFAULT true,
    changelog         boolean NOT NULL DEFAULT false,
    surveys           boolean NOT NULL DEFAULT true,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX customer_notification_preferences_by_workspace
    ON customer_notification_preferences (workspace_id, updated_at DESC);
