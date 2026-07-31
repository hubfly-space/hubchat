-- 0022_feedback_links_unique.sql
-- Make feedback linking idempotent under concurrent dashboard retries.

CREATE UNIQUE INDEX feedback_links_unique_target
    ON feedback_links (
        workspace_id,
        item_id,
        coalesce(conversation_id, ''),
        coalesce(ticket_id, '')
    );
