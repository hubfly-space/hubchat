-- Keep the request correlation id beside the customer event shown in the
-- developer explorer. Nullable preserves events created by the widget and
-- older SDK paths that do not have an HTTP request context.
ALTER TABLE customer_events
    ADD COLUMN request_id text;

CREATE INDEX customer_events_by_request_id
    ON customer_events (workspace_id, request_id, occurred_at DESC)
    WHERE request_id IS NOT NULL;
