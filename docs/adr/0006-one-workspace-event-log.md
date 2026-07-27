# ADR-0006: One workspace event log

**Status:** Accepted

## Context

Five subsystems need to know what happened, in order:

- **Realtime resume (§9).** A reconnecting client asks for what it missed.
- **Webhook delivery (§6.16).** An endpoint receives events, with retry and replay.
- **Automation triggers (§6.13).** Rules fire on `message.received`, `ticket.updated`, and friends.
- **Notifications (§6.15).** Assignment and mention alerts fan out to members.
- **Analytics (§6.18).** Reports are a deterministic aggregation over stored events.

The obvious path is to let each read what it needs from the operational tables:
realtime broadcasts from the conversation service, webhooks poll for changed
rows, automation hooks into service methods, and so on.

That path was tried in the original conversation service, which called a
`Broadcaster` interface directly after committing. It works for exactly one
consumer on exactly one process.

## Decision

Every state change worth telling anyone about is appended to one table,
`workspace_events`, carrying a per-workspace monotonic `sequence`. All five
consumers read from it.

Appends happen inside the transaction that made the change, via
`events.Log.Append(ctx, tx, event)`.

The sequence is allocated from a dedicated `workspace_event_sequences` counter
row, whose lock is held until the caller's transaction commits.

Cross-process delivery uses `LISTEN/NOTIFY`, consistent with ADR-0002. The
notification carries only `workspace_id:sequence`; subscribers read the rows
themselves.

## Consequences

**A broadcast is not durable; an event is.** The old `Broadcaster` call reached
whoever happened to be connected to that process at that instant. A client
that reconnected one second later had no way to learn what it missed, because
nothing had recorded it. Resume is not a feature that can be added on top of
broadcasting — it requires the record to exist.

**Sequence order equals commit order.** This is why the counter lock is held to
commit rather than released immediately. Without it, a reader could observe
sequence 10 committed while 9 was still in flight, resume from 10, and never
see 9. A silently dropped message is the worst failure this system can have,
and it would be invisible in testing and intermittent in production.

The cost is that event appends within one workspace serialise. For a support
product — where one workspace's writes are human-paced — this is the right
trade. The counter lives in its own table specifically so that this lock does
not collide with unrelated reads of `workspaces`.

**Gaps are acceptable; duplicates and reordering are not.** A rolled-back
transaction may burn a sequence number. That is harmless: consumers ask for
"everything after N" and a missing number changes nothing. What must never
happen is two events sharing a number, or a lower number committing after a
higher one.

**Publication becomes a boundary.** An event's `data` is delivered to webhook
consumers and browsers verbatim, so it carries the API's field names, not the
repository's, and only what those consumers may see (§12). This is enforced by
convention rather than by types, which is a known weakness — a golden-JSON
contract test is the intended backstop.

**One place to add a consumer.** Adding an integration means reading the log,
not editing every service that might be interesting.

## Alternatives considered

**Per-feature wiring.** Rejected: no reliable realtime resume, webhook replay
becomes guesswork, and automation triggers scatter across every service. The
five consumers would drift because nothing forces them to agree.

**A message broker.** Rejected: ADR-0002 keeps PostgreSQL as the coordination
layer so the deployment stays one binary and a database. A broker would be a
second thing to operate for a capability Postgres already provides at the
scale this targets.

**Deriving events from a change-data-capture stream.** Rejected: logical
decoding would produce row changes, not domain events. `conversation.resolved`
is not recoverable from an `UPDATE` on a status column without re-implementing
the domain logic in the consumer.
