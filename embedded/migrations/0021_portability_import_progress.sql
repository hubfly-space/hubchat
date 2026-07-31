-- §6.20 resumable workspace imports. Progress is separate from the request
-- metadata so each committed batch can advance independently and be resumed
-- after a worker lease expires or a transient database failure.
BEGIN;

CREATE TABLE import_request_progress (
    import_id    text PRIMARY KEY REFERENCES import_requests (id) ON DELETE CASCADE,
    table_index  integer NOT NULL DEFAULT 0,
    row_index    integer NOT NULL DEFAULT 0,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT import_request_progress_position CHECK (table_index >= 0 AND row_index >= 0)
);

COMMIT;
