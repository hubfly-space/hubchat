-- 0023_ticket_import_keys.sql
--
-- A display number is allocated by the workspace and is not a safe source
-- identifier for imports. Keep the source row key separately so a resumed
-- import can reuse the ticket it already created.

BEGIN;

ALTER TABLE tickets
    ADD COLUMN import_key text;

CREATE UNIQUE INDEX tickets_by_import_key
    ON tickets (workspace_id, import_key)
    WHERE import_key IS NOT NULL;

COMMIT;
