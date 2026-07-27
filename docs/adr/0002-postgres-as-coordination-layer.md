# ADR-0002 — PostgreSQL as the only required dependency

**Status:** Accepted · 2026-07-26

## Context

Hubchat needs a job queue, a scheduler, distributed locks, realtime fan-out, and
idempotency records. The reflex answer is Redis for the first four and a broker
for the fifth.

Every dependency added here is one a self-hoster must install, monitor, back up,
upgrade, and debug at 2am. Most Hubchat deployments will be a single team
supporting a single product.

## Decision

PostgreSQL provides all of it. Redis and message brokers are not required, and
not optional-but-recommended — they are absent.

| Need | Mechanism |
|---|---|
| Durable job queue | `jobs` table, leased with heartbeat |
| Scheduled work | `scheduled_at` index, polled |
| Distributed locks | Advisory locks |
| Leader election | Advisory locks |
| Realtime fan-out | `LISTEN` / `NOTIFY` |
| Idempotency | Unique constraints |
| Rate limiting | Counter rows with careful design |

## Consequences

**Good.** One thing to install, one thing to back up, one thing to restore. Jobs
are transactional with the data that created them — a webhook is enqueued in the
same transaction as the ticket, so it cannot fire for a ticket that rolled back.
Everything is inspectable with SQL, which matters a great deal at 2am.

**Bad.** A polled queue has latency a pushed one does not. High-volume event
ingestion competes with support queries for the same database — mitigated by
batching, retention, and careful indexing, and the reason event limits exist.
`LISTEN`/`NOTIFY` will not carry a very large multi-node realtime deployment.

**The ceiling is deliberate and acknowledged.** When someone reaches it, an
adapter goes behind the existing jobs and realtime interfaces. That is a change
for the installation that needs it, not a tax on the hundreds that do not.

## Alternatives

**Redis for queue and pub/sub.** Rejected: a second stateful dependency for
every install, to solve a problem most installs do not have.

**An embedded queue (SQLite, BadgerDB).** Rejected: breaks the multi-node story
entirely and adds a second durability model to reason about.
