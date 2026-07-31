-- 0015_sla_runtime_guards.sql
-- Runtime bookkeeping for business-hours SLA timers.
BEGIN;

ALTER TABLE sla_instances
    ADD COLUMN IF NOT EXISTS running_since timestamptz;

CREATE UNIQUE INDEX IF NOT EXISTS sla_instances_one_conversation_kind
    ON sla_instances (conversation_id, kind)
    WHERE conversation_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS sla_instances_one_ticket_kind
    ON sla_instances (ticket_id, kind)
    WHERE ticket_id IS NOT NULL;

COMMIT;
