# Public API

Hubchat publishes the versioned REST contract as [OpenAPI 3.1](../embedded/openapi.json).
Running deployments serve the exact contract embedded in the binary at
`/api/v1/openapi.json`.

The API is workspace-scoped. Browser clients normally authenticate with the
Hubchat session cookie; server integrations use a workspace-scoped API key in
the `Authorization: Bearer ...` header and may select the workspace explicitly
with `Hubchat-Workspace-Id`.

## SCIM member provisioning

Directory providers use a workspace API key whose scopes include
`member.manage` with the SCIM 2.0 resource family:

```text
/api/v1/scim/v2.0/{workspaceID}/Users
/api/v1/scim/v2.0/{workspaceID}/Users/{memberID}
```

The implementation supports list, create, replace, patch, and delete. List
requests accept SCIM `startIndex`, `count`, and exact `userName`/`externalId`
filters. `externalId` is the idempotency key; retries reconcile the existing
workspace membership instead of creating a second user. `DELETE` and
`active:false` deprovision without deleting the membership row, preserve the
audit/event history, reject deactivation of the workspace owner, and revoke
the member's sessions, trusted devices, and workspace API keys issued by that
member. A SCIM key is rejected when its workspace path does not match the key's
workspace, even if the caller knows another workspace ID.

## Workspace custom roles

`GET /api/v1/roles` returns the six built-in roles plus the current workspace's
custom roles. Members with `member.manage` can create, update, and delete
custom roles through `POST /api/v1/roles`, `PATCH /api/v1/roles/{id}`, and
`DELETE /api/v1/roles/{id}`. Custom keys are immutable after creation and use
lowercase `snake_case` identifiers. Capability names are validated against the
server catalog, role changes are audited and published as workspace events,
and a role cannot be deleted while a member still uses it. Member role changes
and invitations resolve roles inside the current workspace, so a role ID or
key from another tenant cannot be assigned or modified.

All errors use the standard envelope described in
[`api-conventions.md`](api-conventions.md), including a request ID. List
endpoints use opaque cursor pagination. Retryable creates, updates, and actions
should send an `Idempotency-Key`. Successful replays include
`Idempotent-Replay: true`; reuse with a different body is rejected, and
concurrent attempts return `409 idempotency_in_flight`.

The automation content endpoints separate safe support use from configuration:
`GET /api/v1/automation/replies` and its `/{id}/use` action require
`conversation.reply`, so agents can use approved saved replies without
receiving `automation.manage`. Creating or changing saved replies remains
restricted to automation managers, and personal/team replies are filtered by
the authenticated member's scope. Macro configuration is restricted to
automation managers; personal and team macros are filtered by member scope,
and execution enforces the capability required by every configured action.
Saved-reply bodies may use `{{customer.name}}` and
`{{ticket.number}}`; the dashboard expands those variables when an agent
inserts a reply into the composer.

The `GET /api/v1/customers/export` and `GET /api/v1/companies/export` endpoints
return authenticated, workspace-scoped CSV snapshots. Customer exports require
the `customer.read_sensitive` capability and are capped at 10,000 rows; use the
background portability archive for larger or complete workspace transfers.

Retention administrators can create an audited legal hold with
`POST /api/v1/workspace/legal-holds` using `{"category":"all|events|sessions|webhooks|surveys|audit","reason":"..."}`.
An active `all` hold protects every configured retention sweep; category-specific
holds protect only their matching data category. Holds are listed with cursor
pagination at `GET /api/v1/workspace/legal-holds` and remain
in history after `POST /api/v1/workspace/legal-holds/{id}/release`. Releasing a
hold resumes future retention sweeps and never restores records already
deleted. These endpoints require `workspace.manage_security` and are always
workspace-scoped.

Portability imports are deliberately two-step: upload a workspace-owned file
through `POST /api/v1/portability/import-files`, create an import request with
`auto_start: false`, preview it with
`POST /api/v1/portability/imports/{id}/preview`, then confirm it with
`POST /api/v1/portability/imports/{id}/confirm` and
`{"backup_verified":true}`. `kind` may be `workspace` for a JSON gzip archive,
`customers_csv`, `companies_csv`, `tickets_csv`, `feedback_csv`, or `knowledgebase_markdown`. CSV imports require stable `external_id`
values when available; rows without one receive a deterministic request-scoped
identity so worker retries cannot duplicate them. The importer records a
table/row cursor and resumes idempotently after a worker retry. Archive files
are checksum-verified, workspace-scoped, and limited to 512 MiB compressed;
CSV files are limited to 100 MiB. `tickets_csv` is also supported and requires
`title` and a workspace-local `inbox_id`; it accepts optional description,
status, priority, type, customer/company/member/team IDs, channel, and `due_at`
columns. Its `external_id` becomes the ticket's immutable import key.
`feedback_csv` requires `board_id` and `title`; it accepts description, type,
status, visibility, submitter/company IDs, product area, priority, and
`external_id`. Feedback rows are keyed by workspace, board, and import key.
Markdown imports are one article per file and require YAML front matter with
`knowledge_base_id`; `title`, `slug`, `language`, `state`, `excerpt`, and
`collection_id` are supported. Articles are upserted by workspace, knowledge
base, language, and slug. Workspace archives do not import users or workspace
members: missing member-owned followers, drafts, and notification preferences
are skipped, while nullable assignee/author references are cleared safely.

The checked-in document is generated from
[`openapi.template.json`](../embedded/openapi.template.json) with
`node scripts/generate-openapi.mjs`; `make openapi-check` validates the
published artifact. Generation also discovers every `/v1` route registered by
the Go HTTP mux. Hand-authored template entries provide the detailed schemas,
examples, and tags; newly added routes receive a deliberately conservative
baseline operation so a shipped endpoint cannot be absent from the public
contract. A baseline entry is a documentation signal, not permission to rely
on an untyped payload: add the endpoint's request and response schemas to the
template before publishing a stable integration.

The core support resources publish typed models for conversations, messages,
tickets, customers, saved replies, cursor pages, and their primary write
inputs. `scripts/generate-ts-api.mjs` derives the additive browser artifact at
`web/shared/src/types/generated-api.ts` and an operation catalog from the
generated OpenAPI document. Run `make openapi-check` after changing the
contract; it regenerates and validates both artifacts. Existing dashboard
imports remain compatible while endpoint clients migrate to the generated
`ApiRequest<OperationId>` and `ApiResponse<OperationId>` types.

Macros are human-triggered actions stored within a workspace. `POST
/v1/automation/macros/{id}/use` requires `subject_type` (`conversation` or
`ticket`) and `subject_id`; the server checks personal/team visibility and
every action's capability before executing anything. Macro reply text is sent
as a conversation reply, while state-changing actions use the same automation
action engine as deterministic rules. `GET`, `PATCH`, and `DELETE`
`/v1/automation/macros/{id}` provide the scope-safe configuration lifecycle;
the dashboard action editor persists the same action vocabulary used by rules.

The embeddable browser contract is documented separately in
[`widget-sdk.md`](widget-sdk.md), including the typed `window.Hubchat` API and
the signed-identity and metadata rules for widget visitors.

Public knowledge-base search returns the same data, has_more, and next_cursor
envelope as authenticated list endpoints. Its cursor preserves full-text
relevance, publication time, and article ID ordering; clients must pass it
back opaquely.

Survey definitions and widget feedback items use the same opaque cursor
contract. GET /api/v1/surveys and
GET /api/v1/widget/feedback/boards/{slug}/items accept limit and cursor;
the widget endpoint also preserves the requested status, sort, search, and
visitor identity across pages.

Conversation message history is bounded as well. The authenticated and widget
message endpoints return the newest window by default, expose `has_more` and
`next_cursor` for older history, and accept the cursor opaquely as `cursor`.
The existing `after` sequence parameter remains available for realtime HTTP
fallback and resume; `before` is retained as a compatibility alias for older
clients.

Conversation relationships use the workspace-scoped
`/api/v1/conversations/{id}/links` family. `POST` accepts a `target_id` and a
relation of `related`, `duplicate_of`, or `follow_up`; `related` links are
canonicalised so retries or reversed requests cannot create duplicates, while
the directional relations preserve their source and target. `GET` returns
every link touching the conversation, and `DELETE` accepts the target ID plus
the relation query parameter. Link creation and removal are idempotent,
audited, and published as conversation events; a target from another
workspace is rejected.

Conversation and ticket follower endpoints are also cursor-paginated. They
return member IDs ordered by their opaque member identifier; pass the returned
`next_cursor` unchanged to continue through a large follower set.

Customer attribute definitions and ticket field definitions use the same page
envelope. Attribute cursors preserve key/id order; field cursors preserve the
configured position/id order, so schema editors can safely load and reorder
large definitions sets.

Workspace archive exports support `POST /api/v1/portability/exports/preview`
before `POST /api/v1/portability/exports`. The preview is read-only and returns
the included table summaries and estimated row count; it does not create a job
or file. A completed archive is stored in the configured file backend for seven
days, after which the scheduler deletes the archive bytes and retains the
export request as an `expired` audit record. `GET /api/v1/portability/exports/{id}/manifest`
therefore works only while the archive remains available.

Portal custom domains use `POST /api/v1/portals/{id}/domains`, then a TXT record
at `_hubchat-verification.<domain>` containing the returned verification token.
`POST /api/v1/portals/{id}/domains/{domainID}/verify` checks DNS and marks the
domain active only after an exact token match. Verified domains resolve the
portal from the request host; the configured subdomain remains available as a
fallback.
