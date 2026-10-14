CREATE TABLE event_consumer_cursors (
    consumer text NOT NULL,
    workspace_id text NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, workspace_id)
);

-- Existing installations have no durable acknowledgement history. Establish
-- the upgrade boundary without re-sending years of email or running old rules.
-- Events committed after this migration are always recovered from checkpoints.
INSERT INTO event_consumer_cursors (consumer, workspace_id, last_sequence)
SELECT consumer, workspace_id, next_sequence - 1
FROM workspace_event_sequences
CROSS JOIN (VALUES ('webhook'), ('automation'), ('notification'), ('emailchannel'), ('sla')) AS consumers(consumer);

