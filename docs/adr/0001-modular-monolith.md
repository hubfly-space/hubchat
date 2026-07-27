# ADR-0001 — Modular monolith, not microservices

**Status:** Accepted · 2026-07-26

## Context

Hubchat spans a lot of domains: conversations, tickets, customers, feedback,
knowledge base, automation, SLAs, email, webhooks, analytics. That breadth
invites a service-per-domain layout.

But the product's central promise is that a team can run this with one binary
and one database. Microservices are incompatible with that promise before a
single line is written.

## Decision

A modular monolith. One process, one binary, one database, with strict module
boundaries inside `internal/`.

Boundaries are enforced by convention and review rather than by the compiler:

- database access for an entity happens only in its owning module,
- cross-module work is an explicit service call or a domain event,
- handlers hold no business logic.

Scale-out, when needed, comes from running the same binary with a subset of
roles (`--roles=worker,scheduler`) rather than from splitting the codebase.

## Consequences

**Good.** One deployment, one release, one set of logs. Transactions span the
whole domain, so "create the ticket, tag it, start the SLA timer, enqueue the
webhook" is atomic — which in a distributed version needs a saga. Local
development is `make dev`.

**Bad.** Boundaries can be violated without anything failing. This is a real
risk and the reason cross-module queries are a review-stopping issue rather than
a style comment. One slow query can affect the whole process, which is why
bounded queues and statement timeouts are not optional.

**Later.** If a module genuinely needs independent scaling, the boundary rules
mean extracting it is mechanical: its service interface already exists and
nothing else touches its tables.

## Alternatives

**Microservices from the start.** Rejected: it contradicts the single-binary
promise, and the operational burden lands on self-hosters who have one support
team and no platform engineer.

**A monolith with no internal boundaries.** Rejected: it is the same decision
minus the option to change your mind later.
