# ADR-0005 — A two-hue palette

**Status:** Accepted · 2026-07-26

## Context

Support tools accumulate colour. Every state, tag, priority, channel, and
integration wants its own hue, and each addition is individually reasonable.
The result is a screen where nothing stands out because everything does — and
agents look at this screen for eight hours.

## Decision

Two hue families carry the product:

- **Ink** — a cool graphite ramp, ~95% of every screen.
- **Cobalt** — the single accent.

Cobalt is permitted in four situations and no others: the primary action (one
per view), live/realtime state, focus rings, and selection.

Supporting constraints:

- **Status hues are desaturated.** Green, amber, and red sit beside 11.5px body
  text in SLA timers constantly; saturated versions vibrate.
- **Violet has exactly one job** — marking automation-authored timeline entries
  so a rule-generated event is never mistaken for an agent.
- **Tags and charts share a fixed six-slot palette.** Workspaces pick a slot,
  not a colour. This is why a screen with forty tags still looks like one
  product.
- **Priority is encoded by bar count first, colour second**, so it survives
  greyscale, colour-blindness, and a 200ms glance.
- **Only a breaching SLA animates.** If everything pulses, nothing is urgent.

Workspace branding re-tones the widget and portal through
`--hc-accent-brand` and `[data-branded]`. It never touches the agent interface —
a tenant's brand colour is for their customers, not for the person working six
tenants' queues.

## Consequences

**Good.** Hierarchy comes from spacing, weight, and one accent, which is what
makes a dense screen scannable. Adding a feature does not mean picking a colour.
Contrast is verifiable across a small set of pairs rather than a combinatorial
mess.

**Bad.** It looks conservative next to competitors, and some categories that
would be "obviously" colour-coded elsewhere are distinguished by position, icon,
or label instead. Occasionally a designer will want a seventh chart colour and
will have to justify it.

**Enforced by.** Semantic tokens only in components; primitives are off-limits.
A hard-coded hex in a review is a change request.
