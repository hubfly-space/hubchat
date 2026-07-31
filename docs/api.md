# Public API

Hubchat publishes the versioned REST contract as [OpenAPI 3.1](../embedded/openapi.json).
Running deployments serve the exact contract embedded in the binary at
`/api/v1/openapi.json`.

The API is workspace-scoped. Browser clients normally authenticate with the
Hubchat session cookie; server integrations use a workspace-scoped API key in
the `Authorization: Bearer ...` header and may select the workspace explicitly
with `Hubchat-Workspace-Id`.

All errors use the standard envelope described in
[`api-conventions.md`](api-conventions.md), including a request ID. List
endpoints use opaque cursor pagination. Retryable creates, updates, and actions
should send an `Idempotency-Key`. Successful replays include
`Idempotent-Replay: true`; reuse with a different body is rejected, and
concurrent attempts return `409 idempotency_in_flight`.

The `GET /api/v1/customers/export` and `GET /api/v1/companies/export` endpoints
return authenticated, workspace-scoped CSV snapshots. Customer exports require
the `customer.read_sensitive` capability and are capped at 10,000 rows; use the
background portability archive for larger or complete workspace transfers.

The checked-in document is generated from
[`openapi.template.json`](../embedded/openapi.template.json) with
`node scripts/generate-openapi.mjs`; `make openapi-check` validates the
published artifact.
