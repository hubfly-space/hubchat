-- 0003_reseed_builtin_roles.sql
--
-- Restores the six built-in roles and their permissions idempotently.
--
-- Why this migration exists: `roles.workspace_id` carries
-- `REFERENCES workspaces (id) ON DELETE CASCADE` so that a workspace's
-- *custom* roles are cleaned up when it is deleted. Built-in roles share the
-- same table with `workspace_id IS NULL` specifically so they are never
-- workspace-scoped — but TRUNCATE CASCADE does not know that distinction.
--
-- `TRUNCATE workspaces CASCADE` (or any cascade that reaches `workspaces`)
-- empties every table with an FK pointing at it — the *entire* table, not
-- just rows matching a particular workspace, because TRUNCATE has no WHERE
-- clause to give it. That takes `roles` and `role_permissions` down with it,
-- including the global seed rows that have nothing to do with any single
-- workspace.
--
-- This is a real operational hazard, not just a migration-authoring exercise:
-- an operator (or a test harness) truncating workspace data for a clean slate
-- can silently delete the capability model out from under every remaining
-- workspace. The fix that actually closes the hole — moving built-in roles to
-- a table with no FK to workspaces at all — is a schema change for a future
-- migration; this one re-seeds so the immediate damage is repairable with one
-- command: `hubchat migrate`.
--
-- Idempotent throughout, so applying it against an already-seeded database
-- (the common case — it usually runs once, right after 0001) is a no-op.
--
-- The conflict arbiter is the primary key, not `roles`' natural
-- `UNIQUE (workspace_id, key)`. That constraint cannot serve as an arbiter
-- here: built-in roles carry `workspace_id IS NULL`, and a unique index treats
-- NULLs as distinct from each other, so `ON CONFLICT (workspace_id, key)`
-- matches nothing on a seeded database and the insert proceeds into a
-- `roles_pkey` violation instead. The ids are deterministic literals, so
-- arbitrating on `id` is both correct and genuinely idempotent.

BEGIN;

INSERT INTO roles (id, workspace_id, key, name, description, is_builtin) VALUES
    ('rol_owner',     NULL, 'owner',     'Owner',     'Full control, including destructive workspace operations.', true),
    ('rol_admin',     NULL, 'admin',     'Admin',     'Manages people, surfaces, and integrations.',               true),
    ('rol_manager',   NULL, 'manager',   'Manager',   'Runs queues, assignments, SLAs, and reporting.',            true),
    ('rol_agent',     NULL, 'agent',     'Agent',     'Reads and replies to conversations in permitted inboxes.',  true),
    ('rol_developer', NULL, 'developer', 'Developer', 'Integrations and technical configuration.',                 true),
    ('rol_analyst',   NULL, 'analyst',   'Analyst',   'Read-only access to reports and records.',                  true)
ON CONFLICT (id) DO NOTHING;

INSERT INTO role_permissions (role_id, capability) VALUES
    ('rol_admin', 'conversation.read'), ('rol_admin', 'conversation.reply'),
    ('rol_admin', 'conversation.assign'), ('rol_admin', 'customer.read'),
    ('rol_admin', 'ticket.manage'), ('rol_admin', 'widget.manage'),
    ('rol_admin', 'portal.manage'), ('rol_admin', 'knowledgebase.manage'),
    ('rol_admin', 'feedback.moderate'), ('rol_admin', 'automation.manage'),
    ('rol_admin', 'sla.manage'), ('rol_admin', 'integration.manage'),
    ('rol_admin', 'report.read'), ('rol_admin', 'audit.read'),
    ('rol_admin', 'member.manage'), ('rol_admin', 'workspace.manage'),

    ('rol_manager', 'conversation.read'), ('rol_manager', 'conversation.reply'),
    ('rol_manager', 'conversation.assign'), ('rol_manager', 'customer.read'),
    ('rol_manager', 'ticket.manage'), ('rol_manager', 'sla.manage'),
    ('rol_manager', 'automation.manage'), ('rol_manager', 'report.read'),

    ('rol_agent', 'conversation.read'), ('rol_agent', 'conversation.reply'),
    ('rol_agent', 'customer.read'),

    ('rol_developer', 'integration.manage'), ('rol_developer', 'report.read'),

    ('rol_analyst', 'report.read'), ('rol_analyst', 'customer.read')
ON CONFLICT (role_id, capability) DO NOTHING;

COMMIT;
