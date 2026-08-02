-- 0036_tasks_capability.sql
-- Tasks are agent-facing work items. Automation configuration remains guarded
-- by automation.manage; this capability controls reading and completing the
-- resulting work queue.

BEGIN;

INSERT INTO role_permissions (role_id, capability) VALUES
    ('rol_admin', 'task.manage'),
    ('rol_manager', 'task.manage'),
    ('rol_agent', 'task.manage')
ON CONFLICT (role_id, capability) DO NOTHING;

COMMIT;
