-- Notification rows created from committed domain events carry their source
-- event so a consumer restart or duplicate signal cannot create a second
-- in-app alert. Existing notifications remain valid and keep a NULL source.

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS source_event_id text;

CREATE UNIQUE INDEX IF NOT EXISTS notifications_by_source_event
    ON notifications (workspace_id, member_id, source_event_id)
    WHERE source_event_id IS NOT NULL;
