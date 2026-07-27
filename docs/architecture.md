# Architecture

How Hubchat is put together, and why it is shaped this way.

---

## 1. One process, several roles

The default deployment is one binary running everything:

```
                    Browser · Widget · API client
                              │
                          HTTPS / WSS
                              │
        ┌─────────────────────┴─────────────────────┐
        │              hubchat binary               │
        ├───────────────────────────────────────────┤
        │  httpserver   routing, middleware, assets │
        │  auth/authz   who, and may they           │
        │  modules      conversation, ticket, …     │
        │  realtime     WebSocket gateway           │
        │  jobs         durable worker + scheduler  │
        │  database     pool, migrations, locks     │
        └─────────────────────┬─────────────────────┘
                              │
                         PostgreSQL
                              │
              local disk or S3-compatible storage
```

PostgreSQL is the only required dependency. SMTP and object storage are
optional and the product degrades explicitly without them rather than failing.

The same binary can later run a subset of roles for larger installations:

```bash
hubchat serve --roles=http,realtime
hubchat serve --roles=worker,scheduler
```

This matters more than it looks. The alternative — a separate worker binary — is
a second deliverable, a second Dockerfile, a second set of release notes, and a
new class of bug where the two drift apart. One binary with a role flag has none
of that, and the split can be introduced when someone actually needs it rather
than being paid for on day one.

## 2. Modular monolith

`internal/` holds one package per domain module. Each owns its tables and
exposes service methods; nothing reaches into another module's data.

```
accounts  auth  authorization  workspace  inbox  conversation  ticket
customer  widget  portal  form  feedback  knowledgebase  survey
automation  sla  notification  emailchannel  webhook  file  search
analytics  audit  jobs  realtime  database  httpserver  app
```

Four rules, enforced in review:

1. **Database access stays inside the owning module.** A module that needs
   another's data calls a service method. This is the rule that keeps the
   monolith from becoming a ball of mud, and it is also what makes extracting a
   service later a mechanical change rather than an archaeology project.
2. **Every query is scoped by workspace.** See [security.md](security.md).
3. **HTTP handlers hold no business logic.** They decode, call a service, and
   encode. A decision inside a handler cannot be unit-tested without a request,
   and cannot be reused by the API, the worker, or the rules engine.
4. **Cross-module work is an explicit call or a domain event.** Never a shared
   mutable struct, never a reach into another module's tables.

## 3. Frontends

Three browser bundles, compiled at release time and embedded with `go:embed`.

| Bundle | Route | Audience | Budget |
|---|---|---|---|
| `dashboard` | `/app/*` | Agents and admins | Behind auth; loaded once per session |
| `portal` | `/portal/*` | Customers | Public and indexed; route-split |
| `widget` | `/widget/*` | Visitors on other people's sites | Tightest — see below |

### Why three and not seven

§8.2 of the product brief suggests separate bundles for the dashboard, agent
inbox, portal, feedback board, knowledge base, widget, and setup. We merged them
to three:

- **Dashboard + inbox + setup** share the shell, the auth state, and most of the
  component library. Splitting them would duplicate all three across bundles to
  save a route.
- **Portal + feedback + knowledge base** are one product to the customer. §6.5
  already specifies that a portal *contains* knowledge-base browsing and
  feedback access, so they were never really separate.
- **Widget** stays alone, because its constraints are genuinely different: it
  runs on someone else's page, and every kilobyte is theirs, not ours.

### The widget's budget

The loader (`public/v1.js`) is hand-written, ships no framework, and is ~4.6 KB.
It is the only thing that touches a host page before someone clicks. It fetches
the public configuration, honours the trigger, and only then imports the
interface. A domain that is not allowlisted never downloads the interface at
all.

The interface itself mounts into a shadow root. That buys style isolation in
both directions and confines us to exactly one node in the host's DOM.

## 4. Realtime

WebSocket, with HTTP fallback for anything essential (§18 degraded modes).

The reliability model rests on two counters:

- **Per-conversation `sequence`** orders messages. Ordering never relies on
  timestamps — two messages can share a microsecond, and clock skew is real.
- **Client-generated message ids** make sends idempotent. A retry after a
  dropped acknowledgement is a no-op, enforced by a unique index rather than by
  a check-then-insert race.

Outbound queues are bounded and slow clients are disconnected. An unbounded
queue turns one stalled browser tab into a server-wide memory leak.

## 5. PostgreSQL as coordination layer

No Redis, no broker. PostgreSQL provides:

| Need | Mechanism |
|---|---|
| Durable job queue | `jobs` table with lease + heartbeat |
| Scheduled work | `scheduled_at` index, polled |
| Singleton tasks | Advisory locks |
| Migration safety | Advisory lock around the run |
| Realtime fan-out | `LISTEN`/`NOTIFY` at moderate scale |
| Idempotency | Unique constraints on idempotency keys |

This is a deliberate ceiling, not an oversight. `LISTEN`/`NOTIFY` will not carry
a very large multi-node realtime deployment, and when it stops being enough an
adapter goes behind the same interface. Adding Redis on day one would make every
self-hosted install carry an operational burden that almost none of them need.

## 6. Data model conventions

- Prefixed, ULID-shaped text ids (`wrk_`, `cnv_`, `tkt_`). Self-describing in
  logs, sortable by creation time, and a mis-joined query fails loudly instead
  of silently matching.
- `workspace_id` on every tenant-owned table, as the **leading** column of the
  primary lookup index. A query that forgets it is then slow enough to notice.
- `timestamptz`, always UTC.
- Partial indexes for active queues — closed and spam rows are the majority
  after a year and appear in none of the hot views.
- JSONB for bounded, schema-validated payloads. Not as a substitute for a
  relation.
- Append-only history tables (`*_status_history`, `audit_logs`) are never
  updated. Reporting derives time-in-state from them.
- No soft deletes. Deletion is either real or it is anonymisation, and §12
  requires the interface to say which.

## 7. Request lifecycle

```
request
  └─ RequestID        assign or accept X-Request-Id
     └─ Recover       panic → 500, logged once with a stack
        └─ Logger     one structured line; no bodies, no query strings
           └─ SecurityHeaders(surface)
              └─ MaxBytes
                 └─ route
                    ├─ /api/*      → handler → service → repository
                    ├─ /app/*      → dashboard SPA
                    ├─ /portal/*   → portal SPA
                    └─ /widget/*   → static files
```

The request id ties together the error a user saw, the server log line, the
audit entry, and any webhook it caused. It is on every API response by
construction, not by handler discipline.

## 8. Asset caching

Two policies, and the split is what makes deploys safe:

- **Content-hashed assets** — `immutable, max-age=31536000`. The filename
  changes when the bytes do.
- **HTML entry points** — `no-cache, must-revalidate`. This file names the
  current asset hashes; a stale copy points at assets that no longer exist.
- **Stable-URL files** (widget loader, favicons) — `max-age=300`. Short enough
  to roll out a fix, long enough not to re-fetch constantly.

## 9. What is deliberately absent

| Not present | Why |
|---|---|
| AI features | Product decision (§3.6). Answers stay traceable to a person or a rule. |
| Redis | PostgreSQL covers the need at the scale this deployment model targets. |
| Message broker | Same. An adapter can be added behind the jobs interface. |
| Node.js in production | Frontends are compiled at build time and embedded. |
| Microservices | A modular monolith with enforced boundaries, extractable later. |
| Soft deletes | Ambiguous. Deletion is real, or it is anonymisation, and we say which. |
| A charting library | Hand-drawn SVG, so charts re-theme with the product and cost the widget nothing. |
