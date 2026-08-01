-- Durable round-robin position. The team row is locked while a cursor is
-- consumed, so concurrent conversation starts cannot select the same slot.
CREATE TABLE team_routing_cursors (
    team_id            text PRIMARY KEY REFERENCES teams (id) ON DELETE CASCADE,
    next_member_index  bigint NOT NULL DEFAULT 0,
    updated_at         timestamptz NOT NULL DEFAULT now()
);
