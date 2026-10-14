ALTER TABLE inboxes ADD COLUMN visibility text NOT NULL DEFAULT 'workspace'
    CHECK (visibility IN ('workspace','teams'));
CREATE INDEX conversations_workspace_priority_activity
    ON conversations (workspace_id, (CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END), last_message_at DESC, id DESC);
