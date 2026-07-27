# ADR-0003 — No AI features

**Status:** Accepted · 2026-07-26

## Context

Every competing support platform ships AI reply suggestions, summarisation,
sentiment scoring, and chatbots. Not shipping them is a conspicuous choice that
will be questioned by contributors and users, repeatedly. This ADR exists so the
answer is written down once.

## Decision

Hubchat ships no AI functionality. Specifically, none of:

- generated replies or reply suggestions
- conversation or ticket summarisation
- chatbots or deflection bots
- automatic classification, tagging, or routing by model
- sentiment analysis
- semantic or embedding-based search
- agent assistance or "copilot" surfaces

Productivity comes from fast search, macros, saved replies, deterministic rules,
structured metadata, saved views, keyboard-first design, and getting the
customer context in front of the agent before they type.

No delivery phase introduces AI.

## Rationale

**Traceability.** A support answer should be attributable to a person or to a
rule someone wrote. "The system suggested it" is not an answer a customer can
escalate against, and not one a support manager can coach.

**Determinism.** The rules engine, SLA timers, routing, and search are all
deterministic. The same input produces the same output, which is what makes the
execution log a debugging tool rather than a narrative.

**Self-hosting.** An AI feature means either an outbound dependency on a
third-party API — unacceptable for the deployments that chose self-hosting
precisely to avoid that — or shipping model weights in a binary that is
currently 18 MB.

**Privacy.** Hubchat stores other people's customer conversations. Sending them
to an inference endpoint is a data-processing decision no default should make on
a workspace's behalf.

**Focus.** The differentiator is customer context, deployment simplicity, and
interface quality. Competing on model quality against companies with inference
budgets is not a fight worth picking.

## Consequences

**Good.** Every behaviour is explainable. No inference costs, no rate limits, no
model deprecations, no prompt regressions. The privacy story is simple enough to
state in one sentence.

**Bad.** We will lose evaluations to feature-matrix comparisons. Some support
volume that a bot would deflect will reach a human. Some contributors will
propose AI features; those PRs get closed with a link here.

**Note.** This is not a claim that AI is useless in support. It is a claim about
what *this* product is for. An integration built on the public API by someone
who wants it is entirely reasonable — it is just not in the binary, and the
workspace opts into it explicitly.
