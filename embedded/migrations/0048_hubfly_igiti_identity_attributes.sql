-- Hubfly space parity for the igiti.net identity keys.
--
-- 0046 declared phone/account_id on the Igiti net workspace only, but the
-- igiti.net frontend was booting the Hubfly space widget
-- (pk_eOoRHC...), which is why support agents watching one workspace saw
-- nothing while the other collected the conversations. The frontend now
-- boots the Igiti net key; this seeds Hubfly space as well so an identifier
-- still resolves if that widget is ever used for igiti.net again.
--
-- See 0046 for why this joins workspaces rather than inserting unconditionally.

INSERT INTO attribute_definitions (
    id, workspace_id, entity_type, key, label, type, description,
    allowed_sources, sensitive, searchable
)
SELECT v.id, v.workspace_id, v.entity_type, v.key, v.label, v.type, v.description,
       v.allowed_sources, v.sensitive, v.searchable
FROM (VALUES
    (
        'atr_797zmfszmfzwcxsjga2m859rwz',
        'wrk_01kyz894q7hwrxk2zst7y9yyr4',
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
        'atr_q1yh911hy7m43zkbhbprtf8c6b',
        'wrk_01kyz894q7hwrxk2zst7y9yyr4',
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