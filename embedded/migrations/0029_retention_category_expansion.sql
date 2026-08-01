-- 0029_retention_category_expansion.sql
-- Extend legal holds to every high-volume/privacy retention category.

BEGIN;

ALTER TABLE workspace_legal_holds
    DROP CONSTRAINT workspace_legal_holds_category_check;

ALTER TABLE workspace_legal_holds
    ADD CONSTRAINT workspace_legal_holds_category_check
    CHECK (category IN ('all', 'events', 'sessions', 'webhooks', 'surveys', 'audit'));

-- Earlier dashboard builds used shorter labels for these JSON retention keys.
-- Preserve those operator settings while moving them to the names consumed by
-- the retention workers.
WITH normalized AS (
    SELECT id,
           (settings #> '{privacy,retention_days}') AS days
    FROM workspaces
    WHERE (settings #> '{privacy,retention_days}') ?| ARRAY['audit', 'webhooks', 'surveys']
)
UPDATE workspaces w
SET settings = jsonb_set(
    w.settings,
    '{privacy,retention_days}',
    (
        days
        || CASE WHEN days ? 'audit_logs' OR NOT days ? 'audit' THEN '{}'::jsonb ELSE jsonb_build_object('audit_logs', days->'audit') END
        || CASE WHEN days ? 'webhook_deliveries' OR NOT days ? 'webhooks' THEN '{}'::jsonb ELSE jsonb_build_object('webhook_deliveries', days->'webhooks') END
        || CASE WHEN days ? 'survey_responses' OR NOT days ? 'surveys' THEN '{}'::jsonb ELSE jsonb_build_object('survey_responses', days->'surveys') END
        - ARRAY['audit', 'webhooks', 'surveys']::text[]
    ),
    true
)
FROM normalized
WHERE w.id = normalized.id;

COMMIT;
