# API conventions

`/api/v1`. These rules are a contract: an SDK, a webhook consumer, and the
dashboard all depend on them holding everywhere.

---

## 1. Shape

Resource-oriented paths, plural nouns:

```
GET    /api/v1/conversations
POST   /api/v1/conversations
GET    /api/v1/conversations/{id}
PATCH  /api/v1/conversations/{id}
POST   /api/v1/conversations/{id}/messages
```

Actions that are not CRUD become sub-resources rather than verbs in the path:

```
POST /api/v1/conversations/{id}/resolve
POST /api/v1/tickets/{id}/merge
POST /api/v1/webhooks/{id}/deliveries/{delivery_id}/replay
```

## 2. Identifiers

Opaque, prefixed, sortable: `cnv_01hq7xk2m9`.

**Never parse an id.** The prefix exists so a human reading a log knows what
they are looking at, and so a mis-joined query fails loudly instead of matching
silently.

Ticket *display numbers* (`SUP-1042`) are a separate, user-facing concept. The
immutable id and the display number are never conflated.

## 3. Authentication

```http
Authorization: Bearer hc_live_9f2a…
```

Keys are workspace-scoped and carry explicit capabilities. Only a hash is
stored; the full key is shown exactly once, at creation.

Browser sessions use cookies with CSRF protection. The widget uses a scoped
visitor token — never a session cookie.

## 4. Pagination

Cursor-based. There are no page numbers.

```http
GET /api/v1/conversations?limit=50&cursor=eyJpZCI6…
```

```json
{ "data": [...], "next_cursor": "eyJpZCI6…", "has_more": true }
```

Offset pagination skips and duplicates rows when the underlying set changes
between requests — which, in an inbox, is constantly.

## 5. Idempotency

Every create accepts:

```http
Idempotency-Key: 0d4f1a2c-9b3e-4f10-a5c2-77b1de3a9e04
```

A repeated key within the retention window returns the original response.
Clients should retry on network failure; without this, a retried "send message"
posts twice, and the customer sees it.

## 6. Errors

One shape, always:

```json
{
  "error": {
    "code": "ticket_not_found",
    "message": "The requested ticket was not found.",
    "request_id": "req_01hq7xk2m9",
    "details": {}
  }
}
```

- `code` is stable and machine-readable. **Renaming one is a breaking change.**
- `message` is human-readable and safe to show a customer.
- `details` carries field-level validation problems.

| Status | Meaning |
|---|---|
| 400 | Malformed request |
| 401 | Missing or invalid credentials |
| 403 | Authenticated, not permitted |
| 404 | Not found, **or found but not yours** |
| 409 | Conflict; version mismatch |
| 422 | Semantically invalid |
| 429 | Rate limited |

404 for cross-tenant access is deliberate. A 403 confirms the record exists,
which is an information leak across a tenant boundary.

## 7. Headers

| Header | Direction | Purpose |
|---|---|---|
| `X-Request-Id` | both | Correlation; echoed on every response |
| `Idempotency-Key` | request | Safe retries on create |
| `X-RateLimit-Limit` | response | Window ceiling |
| `X-RateLimit-Remaining` | response | Remaining in window |
| `X-RateLimit-Reset` | response | Unix seconds until reset |

## 8. Events and webhooks

One envelope for realtime frames and webhook bodies alike:

```json
{
  "id": "evt_01hq7xk2m9",
  "type": "ticket.sla_breached",
  "workspace_id": "wrk_01hq7x",
  "occurred_at": "2026-07-26T12:00:00Z",
  "sequence": 1042,
  "data": { }
}
```

Deliveries carry:

```http
Hubchat-Signature: v1=5257a869e7ecebeda32affa62cdca3fa
Hubchat-Timestamp: 1774526400
```

Verify **both**. Signed payload is `timestamp + "." + raw body` — the exact
bytes received, before JSON parsing. Reject timestamps older than five minutes
and compare digests in constant time. A valid signature on a replayed request is
still a replayed request.

Endpoints auto-disable after six consecutive failures so one dead URL cannot
starve the queue. Failed deliveries are replayable for 30 days.

## 9. Compatibility

- Adding a field is compatible. Removing or retyping one is not.
- **Never silently change an event's meaning.** Introduce a new type.
- Deprecations are documented with a date and a migration path.
- Breaking webhook changes are versioned.
- Enums may gain members; clients must tolerate unknown values.

## 10. Rate limits

Per key and per workspace, default 600 requests/minute. Event ingestion has its
own higher, separately-tracked limit — a burst of `page.viewed` must not exhaust
the budget for replying to customers.
