-- 0027_customer_360_indexes.sql
-- Query-path indexes for the workspace-scoped customer 360 view.

BEGIN;

CREATE INDEX survey_responses_by_customer
    ON survey_responses (workspace_id, customer_id, submitted_at DESC, id)
    WHERE customer_id IS NOT NULL AND submitted_at IS NOT NULL;

CREATE INDEX article_feedback_by_customer
    ON article_feedback (workspace_id, customer_id, created_at DESC, id)
    WHERE customer_id IS NOT NULL;

COMMIT;
