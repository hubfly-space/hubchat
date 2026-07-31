-- 0014_survey_capability.sql
-- Survey authoring is a separate capability from read-only reporting.
BEGIN;
INSERT INTO role_permissions (role_id, capability) VALUES
    ('rol_admin', 'survey.manage'),
    ('rol_manager', 'survey.manage')
ON CONFLICT (role_id, capability) DO NOTHING;
COMMIT;
