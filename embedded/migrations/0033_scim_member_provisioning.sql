-- 0033_scim_member_provisioning.sql
-- SCIM deprovisioning is reversible and must not erase member-owned history.

BEGIN;

ALTER TABLE workspace_members
    ADD COLUMN scim_external_id text,
    ADD COLUMN deactivated_at timestamptz;

CREATE UNIQUE INDEX workspace_members_scim_external
    ON workspace_members (workspace_id, scim_external_id)
    WHERE scim_external_id IS NOT NULL;

CREATE INDEX workspace_members_active_by_workspace
    ON workspace_members (workspace_id, created_at DESC)
    WHERE deactivated_at IS NULL;

COMMIT;
