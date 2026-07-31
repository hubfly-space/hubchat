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

Portability imports are deliberately two-step: upload a workspace-owned file
through `POST /api/v1/portability/import-files`, create an import request with
`auto_start: false`, preview it with
`POST /api/v1/portability/imports/{id}/preview`, then confirm it with
`POST /api/v1/portability/imports/{id}/confirm` and
`{"backup_verified":true}`. `kind` may be `workspace` for a JSON gzip archive,
`customers_csv`, `companies_csv`, `tickets_csv`, or `knowledgebase_markdown`. CSV imports require stable `external_id`
values when available; rows without one receive a deterministic request-scoped
identity so worker retries cannot duplicate them. The importer records a
table/row cursor and resumes idempotently after a worker retry. Archive files
are checksum-verified, workspace-scoped, and limited to 512 MiB compressed;
CSV files are limited to 100 MiB. `tickets_csv` is also supported and requires
`title` and a workspace-local `inbox_id`; it accepts optional description,
status, priority, type, customer/company/member/team IDs, channel, and `due_at`
columns. Its `external_id` becomes the ticket's immutable import key.
Markdown imports are one article per file and require YAML front matter with
`knowledge_base_id`; `title`, `slug`, `language`, `state`, `excerpt`, and
`collection_id` are supported. Articles are upserted by workspace, knowledge
base, language, and slug.

The checked-in document is generated from
[`openapi.template.json`](../embedded/openapi.template.json) with
`node scripts/generate-openapi.mjs`; `make openapi-check` validates the
published artifact.

The embeddable browser contract is documented separately in
[`widget-sdk.md`](widget-sdk.md), including the typed `window.Hubchat` API and
the signed-identity and metadata rules for widget visitors.
