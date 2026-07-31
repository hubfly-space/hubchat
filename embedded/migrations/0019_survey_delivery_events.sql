-- 0019_survey_delivery_events.sql
-- Keep one pending survey invitation per source lifecycle event. This makes
-- event-consumer retries safe without suppressing a later resolution after a
-- ticket has been reopened.

ALTER TABLE survey_responses
    ADD COLUMN source_event_id text;

CREATE UNIQUE INDEX survey_responses_source_event
    ON survey_responses (workspace_id, survey_id, source_event_id)
    WHERE source_event_id IS NOT NULL;
