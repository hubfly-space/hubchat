-- 0038_portal_forms_navigation.sql
--
-- Make the published forms surface reachable for portals created before the
-- default navigation entry was added. Existing custom entries are preserved.

BEGIN;

INSERT INTO portal_navigation_items (id, portal_id, label, href, position)
SELECT 'pnv_' || md5(p.id || ':forms'), p.id, 'Forms', '/forms',
       COALESCE((SELECT max(position) + 1 FROM portal_navigation_items existing WHERE existing.portal_id = p.id), 0)
FROM portals p
WHERE NOT EXISTS (
    SELECT 1 FROM portal_navigation_items existing
    WHERE existing.portal_id = p.id AND existing.href = '/forms'
);

COMMIT;
