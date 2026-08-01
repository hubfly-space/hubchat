# Backend guidelines

Go, PostgreSQL, one binary.

---

## 1. Module boundaries

Each `internal/<module>` owns a slice of the domain. Its `doc.go` states its
responsibilities and its boundary. Four rules:

1. **Database access for the entities a module owns happens in that module.**
   Another module calls a service method.
2. **Every query is scoped by workspace.** No exceptions.
3. **HTTP handlers hold no business logic.** Decode, call a service, encode.
4. **Cross-module work is a service call or a domain event.** Never a reach into
   another module's tables.

Rule 1 is the one that gets quietly broken under deadline pressure — usually as
"just this one join". Reviewers should treat a cross-module query as a design
question, not a style nit: it is the difference between a monolith you can
extract from later and one you cannot.

## 2. Layering

```
handler        decode → authorize → call service → encode
service        business rules, transactions, events
repository     SQL, scoped by workspace
```

Services take a `context.Context` carrying the actor, and return domain errors.
They never see an `http.Request` — which is what lets the rules engine, the CLI,
and the API all call the same method.

## 3. Errors

```go
var ErrTicketNotFound = errors.New("ticket not found")

if err := svc.Resolve(ctx, id); err != nil {
    return fmt.Errorf("resolve ticket %s: %w", id, err)
}
```

- Wrap with context on the way up; the top of the stack logs once.
- Sentinel errors for conditions callers branch on.
- Map to the API error contract at the handler boundary, never deeper.
- **A message that reaches a customer never contains a stack trace, a SQL
  fragment, or an internal id.** The request id is how support correlates it.

## 4. Database

### Every query is workspace-scoped

```go
// yes
const q = `SELECT … FROM conversations WHERE workspace_id = $1 AND id = $2`

// no — an id from another tenant now reads across the boundary
const q = `SELECT … FROM conversations WHERE id = $1`
```

`workspace_id` leads every primary lookup index, so a query that forgets it is
slow enough to be noticed in review or in the slow-query log.

### Transactions

One transaction per business operation. Do not span an HTTP call or a file
upload — a transaction held open across a network round trip is a connection
nobody else can use and a lock nobody expected.

### Migrations

- Live in `embedded/migrations/`, applied in filename order.
- **A migration that has shipped is never edited.** Supersede it.
- Additive first: add a column, backfill, switch reads, drop later. A migration
  that rewrites a large table locks it, and locking `conversations` takes
  support offline.
- Index creation on a populated table uses `CONCURRENTLY`, outside a
  transaction.
- Applied under an advisory lock so several instances starting at once apply
  them exactly once.

### Reading the schema

`0001` and `0002` carry the conventions in comments. Two worth internalising:

- **Partial indexes for active queues.** Closed and spam conversations are most
  rows after a year and appear in none of the hot views.
- **Conversation routing is transactional.** When an inbox has a configured
  team, a new conversation is assigned to that team in the same transaction as
  its opening message. Manual and team-queue strategies leave work at the team;
  round-robin consumes the durable `team_routing_cursors` row, while
  least-active, customer-owner, and weighted strategies consider only members
  currently accepting conversations. The team row is locked during selection,
  so concurrent starts cannot make the same routing decision.
- **Every index names the query it exists for.** An index without a named query
  is one nobody will dare delete.

## 5. Concurrency

- Bounded goroutines. An unbounded `go` per request is a memory leak with a
  delay fuse.
- Every blocking operation takes a context and honours cancellation.
- Bounded channels for realtime fan-out; disconnect slow clients rather than
  buffering for them.
- Long-lived goroutines are owned by a supervisor that can stop them on
  shutdown.

## 6. Background jobs

Durable rows in PostgreSQL, leased with a heartbeat.

- Idempotent. A job may run twice; assume it will.
- Retry with exponential backoff up to `MaxAttempts`, then dead-letter.
- Dead-lettered jobs stay visible and retryable, in the dashboard and the CLI.
- Pending jobs can be explicitly cancelled from the dashboard or API; running
  jobs are never force-killed because handlers may hold external side effects.
- **Workspace fairness is explicit.** One tenant's export must not starve
  everyone else's notifications.
- **Export retention is enforced.** Completed workspace archives have a
  seven-day download window; the scheduled portability sweep removes their
  file object but keeps the request row as an `expired` audit record. A failed
  object deletion remains eligible for a later sweep.

## 7. Logging

Structured, via `slog`.

Always: request id, module, workspace id where safe, actor id where safe,
latency, error code.

**Never: message bodies, tokens, passwords, API keys, or sensitive custom
attributes.** A support platform's request bodies are, by definition, other
people's customer conversations. This is why the request logger records only
method and path — not query strings, which carry search terms.

## 8. Configuration

`internal/config` resolves defaults → file → environment → flags.

- `Validate()` reports **every** problem, not the first. An operator fixing
  configuration should not restart six times to find six mistakes.
- Secret-bearing fields are excluded from `Redacted()`, which is what
  `config check` and `doctor` print — that output gets pasted into issue
  reports.
- Migrations default to `verify`, not `apply`. A process restart must never
  silently rewrite a production schema.

## 9. Testing

| Layer | What it covers |
|---|---|
| Unit | Permission checks, state transitions, SLA arithmetic, rule conditions, identity linking, webhook signatures, ticket numbering |
| Integration | Real PostgreSQL: migrations, tenant scoping, transactions, job leasing, advisory locks, full-text search, idempotency |
| End-to-end | Setup, widget install, anonymous and verified conversations, portal reply, feedback voting, webhook delivery, export |
| Security | **Cross-workspace access for every resource type**, privilege escalation, IDOR, forged identity, widget origin bypass, CSRF, XSS, path traversal, webhook replay |
| Realtime | Reconnect, duplicate send, out-of-order events, slow client, server restart, event resume |

The cross-tenant suite is not optional. Every resource type gets a test that
attempts to read it from the wrong workspace and asserts a 404 — a missing
workspace predicate is a critical defect, and this is how it gets caught.

## 10. Naming

- Packages: short, lowercase, no underscores. `conversation`, not `conversations`
  or `conversation_service`.
- No stuttering: `conversation.Service`, not `conversation.ConversationService`.
- Interfaces named for behaviour: `Storer`, `Notifier`.
- Accept interfaces, return structs.
