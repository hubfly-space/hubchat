ALTER TABLE scheduled_actions DROP CONSTRAINT scheduled_actions_state;
ALTER TABLE scheduled_actions ADD CONSTRAINT scheduled_actions_state CHECK(state IN ('pending','running','executed','cancelled','failed'));
ALTER TABLE scheduled_actions ADD COLUMN lease_until timestamptz;
ALTER TABLE scheduled_actions ADD COLUMN attempts integer NOT NULL DEFAULT 0;
CREATE INDEX scheduled_actions_expired_leases ON scheduled_actions(lease_until) WHERE state='running';
