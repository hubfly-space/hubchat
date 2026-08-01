ALTER TABLE teams
    ADD COLUMN routing_config jsonb NOT NULL DEFAULT '{}'::jsonb;
