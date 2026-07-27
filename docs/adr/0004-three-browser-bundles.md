# ADR-0004 — Three browser bundles, not seven

**Status:** Accepted · 2026-07-26

## Context

The product brief (§8.2) suggests separate bundles for: admin dashboard, agent
inbox, customer portal, feedback board, knowledge base, widget, and setup.

More bundles mean smaller individual payloads, but also duplicated shared code,
more build configuration, and more places for the design system to drift.

## Decision

Three bundles.

| Bundle | Contains | Why together |
|---|---|---|
| `dashboard` | Admin, agent inbox, setup, onboarding | Same shell, same auth state, same component library |
| `portal` | Portal, knowledge base, feedback, changelog | One product to the customer; §6.5 already nests them |
| `widget` | Loader + embeddable interface | Genuinely different constraints |

The widget stays separate because it runs on someone else's page. Every
kilobyte is their visitor's, not ours, and it needs shadow-root isolation and a
hand-written loader that the other two do not.

Splitting happens **inside** a bundle by route, not across bundles. The
dashboard's ~70 routes are each their own chunk; the portal splits everything
past the landing page.

## Consequences

**Good.** One component library, one token layer, one build pipeline per
surface. Route-level splitting gives most of the payload benefit without
duplicating React and the design system three more times. An agent moving from
the inbox to settings pays for one small chunk, not a full page load.

**Bad.** The dashboard entry chunk carries the shell for every route, including
the ones a given agent never opens. Measured, this is smaller than the
duplication a finer split would cause.

**Enforced by.** Per-bundle `chunkSizeWarningLimit` values that fail loudly when
someone imports the wrong thing — for instance pulling a dashboard-only
component into the portal.
