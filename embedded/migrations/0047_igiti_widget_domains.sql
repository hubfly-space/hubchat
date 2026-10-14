-- IGITI widget origin allowlist.
--
-- The widget's only entry was the wildcard "*.igiti.net". originAllowed never
-- matches an apex from a wildcard (internal/widget/public.go), so the live
-- site at https://igiti.net was rejected with ErrOriginNotAllowed and the
-- widget never rendered there. The apex, the www host and the local dev
-- server each need their own entry.
--
-- The three igiti hosts on the *Hubfly* widget are left alone: they are a
-- separate workspace's configuration and removing them is a product
-- decision, not a migration.
--
-- The JOIN keeps `migrate` working on databases that never hosted this
-- widget; a tenant-scoped seed that only works on one production account is
-- not a migration anyone else can run.

INSERT INTO widget_domains (id, widget_id, domain, created_at)
SELECT v.id, v.widget_id, v.domain, now()
FROM (VALUES
    ('wgd_gf75nnbhn60b6kw05k7qzhh8x8', 'wgt_01m0r8hn6b49zbefb8bt857fvh', 'igiti.net'),
    ('wgd_f8e2j6828ea59yt8nnt9q2xkhr', 'wgt_01m0r8hn6b49zbefb8bt857fvh', 'www.igiti.net'),
    ('wgd_n5p7kcdxp5rwg3dmdbn0sg9jnr', 'wgt_01m0r8hn6b49zbefb8bt857fvh', 'localhost:3000'),
    ('wgd_v3gf65e40srwb6facjsscagz5s', 'wgt_01m0r8hn6b49zbefb8bt857fvh', 'localhost')
) AS v(id, widget_id, domain)
JOIN widgets w ON w.id = v.widget_id
ON CONFLICT (widget_id, domain) DO NOTHING;