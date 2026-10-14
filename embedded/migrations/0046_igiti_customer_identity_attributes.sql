-- IGITI support identity keys.
--
-- The igiti.net widget identifies a visitor with the site's own account id
-- (external_id) and a single `emailPhone` column that holds *either* an email
-- address or a phone number. At the time this was written the identify
-- endpoint had no top-level phone field, so the phone had to arrive as a
-- declared attribute; without these rows ValidateCustomerAttributes rejects
-- the whole identify payload with ErrAttrNotDeclared and the visitor is never
-- linked to a customer at all (not even the external_id that would have
-- succeeded on its own). 0049 later added the top-level phone field, so these
-- keys are now belt-and-braces for older widget builds still sending them.
--
-- Both keys are non-sensitive on purpose: an agent reading the account id and
-- the contact number is the whole point of the integration, and marking them
-- sensitive would mask them behind a reveal affordance in the inbox sidebar.
--
-- The JOIN is not decoration. A tenant-scoped seed like this must not break
-- `migrate` on a database that has never hosted the igiti workspace — CI, a
-- fresh self-hosted install, and every developer's local hubchat_test all run
-- this file, and a plain INSERT there fails on the workspace_id foreign key
-- and takes the whole migration chain down with it.

INSERT INTO attribute_definitions (
    id, workspace_id, entity_type, key, label, type, description,
    allowed_sources, sensitive, searchable
)
SELECT v.id, v.workspace_id, v.entity_type, v.key, v.label, v.type, v.description,
       v.allowed_sources, v.sensitive, v.searchable
FROM (VALUES
    (
        'atr_0dn5my9cyecbfe9rjbkaqsmgqr',
        'wrk_01m0r8gx3fnvvaktkk02wjrd5w',
        'customer',
        'phone',
        'Phone',
        'string',
        'Contact phone number from the igiti.net account.',
        '["js_sdk","rest_api"]'::jsonb,
        false,
        true
    ),
    (
        'atr_6zssmpwdsh82pxdq7v2csagrsx',
        'wrk_01m0r8gx3fnvvaktkk02wjrd5w',
        'customer',
        'account_id',
        'Account ID',
        'string',
        'igiti.net account id, mirrored from external_id for filtering and search.',
        '["js_sdk","rest_api"]'::jsonb,
        false,
        true
    )
) AS v(
    id, workspace_id, entity_type, key, label, type, description,
    allowed_sources, sensitive, searchable
)
JOIN workspaces w ON w.id = v.workspace_id
ON CONFLICT (workspace_id, entity_type, key) DO NOTHING;