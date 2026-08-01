-- 0034_custom_roles.sql
-- Release the initial built-in-only membership constraint now that
-- workspace-scoped custom roles are supported by the service layer.

BEGIN;

ALTER TABLE workspace_members DROP CONSTRAINT workspace_members_role;
ALTER TABLE workspace_members
    ADD CONSTRAINT workspace_members_role_format
    CHECK (role ~ '^[a-z][a-z0-9_]{1,31}$');

CREATE INDEX roles_by_workspace_created
    ON roles (workspace_id, created_at DESC)
    WHERE workspace_id IS NOT NULL;

COMMIT;

