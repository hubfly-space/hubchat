# Architecture decision records

Decisions that would otherwise be re-argued from memory in six months.

An ADR is cheap to write and expensive not to have. If a choice was
non-obvious, had a plausible alternative, or will be questioned by someone new,
it belongs here.

| # | Decision | Status |
|---|---|---|
| [0001](0001-modular-monolith.md) | Modular monolith, not microservices | Accepted |
| [0002](0002-postgres-as-coordination-layer.md) | PostgreSQL as the only required dependency | Accepted |
| [0003](0003-no-ai-features.md) | No AI features | Accepted |
| [0004](0004-three-browser-bundles.md) | Three browser bundles, not seven | Accepted |
| [0005](0005-two-hue-palette.md) | A two-hue palette | Accepted |

## Still open

Recorded here so they are not forgotten:

- **Licence and edition strategy.** Which components are open, and whether
  hosted-only extensions may exist. Must be settled before the first release.
- **Custom roles.** The capability model already supports them; the interface
  and migration path do not exist yet.
- **Search backend.** PostgreSQL full-text search is the decision for now. The
  trigger for revisiting it should be a measurement, not a hunch.

## Format

```markdown
# ADR-NNNN — Title

**Status:** Proposed | Accepted | Superseded by ADR-MMMM · YYYY-MM-DD

## Context
What made this a decision. The forces, not the solution.

## Decision
What we are doing, stated plainly.

## Consequences
What this buys, what it costs, and what it forecloses. Be honest about the
costs — an ADR that lists only benefits is marketing.

## Alternatives
What else was considered, and the specific reason it lost.
```
