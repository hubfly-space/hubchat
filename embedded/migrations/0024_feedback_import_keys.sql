-- Keep repeated feedback imports idempotent without changing shipped tables.
ALTER TABLE feedback_items ADD COLUMN import_key text;

CREATE UNIQUE INDEX feedback_items_by_import_key
    ON feedback_items (workspace_id, board_id, import_key)
    WHERE import_key IS NOT NULL;
