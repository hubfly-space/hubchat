-- 0040_inbox_queue_indexes.sql
-- Indexes for the workspace inbox's mentioned and SLA queue shortcuts.

BEGIN;

-- The mentioned queue is filtered by the authenticated member and then joins
-- back to a conversation through entity_id. Delivery preferences do not
-- change the durable notification row that backs this view.
CREATE INDEX notifications_mentions_by_member_entity
    ON notifications (workspace_id, member_id, entity_id)
    WHERE type = 'mention' AND entity_type = 'conversation';

-- Only active warned timers and breached timers participate in the inbox
-- shortcuts; satisfied, paused, and cancelled timers never need this path.
CREATE INDEX sla_instances_conversation_queue
    ON sla_instances (workspace_id, conversation_id)
    WHERE state = 'breached' OR (state = 'active' AND warned_at IS NOT NULL);

COMMIT;
