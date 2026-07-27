# Design system

The interface an agent stares at for eight hours. Everything below exists to
keep it calm, dense, and unambiguous.

---

## 1. The three-layer token model

```
tokens.css   primitives      --ink-900, --cobalt-500, --rad-md, --fs-base
     ↓
theme.css    semantics       --hc-surface, --hc-accent, --hc-text-muted
     ↓
index.css    Tailwind bridge bg-surface, text-fg-muted, border-line
```

**Components consume the semantic layer only.** A component that reaches for
`--ink-900` has hard-coded dark mode; one that uses `--hc-surface` re-themes for
free.

Adding a colour means adding a *semantic* token and pointing it at an existing
primitive. If you find yourself adding a primitive, stop and ask whether the
product genuinely needs a new hue — the answer is usually no.

## 2. Colour discipline

**Two hue families carry the entire product.**

- **Ink** — cool graphite, ~95% of every screen.
- **Cobalt** — the single accent.

Cobalt is permitted in exactly four situations:

1. the primary action on a screen (one per view),
2. live and realtime state,
3. focus rings,
4. selection.

If you are reaching for the accent for a fifth reason, what you actually want is
hierarchy through spacing or type weight.

Status hues — green, amber, red — are deliberately desaturated. They sit next to
body text constantly in SLA timers and ticket states, and a saturated red at
11.5px vibrates. Violet exists for exactly one purpose: distinguishing
automation-authored timeline entries from human ones.

**Tags and charts draw from a fixed six-slot palette** (`--hc-chart-1..6`).
Workspaces cannot introduce new hues. This is why a Hubchat screen with forty
tags still looks like one product.

### Contrast

Every text-on-surface pair meets WCAG AA at its intended size. `--hc-text-muted`
is the floor for readable prose; `--hc-text-disabled` is for non-essential text
only, never for content.

## 3. Typography

Sub-pixel steps, because a dense operations UI needs a 13.5px body and a 12.5px
secondary that are distinguishable without a full point of jump.

| Token | Size | Used for |
|---|---|---|
| `text-2xs` | 10.5px | Metadata, timestamps, counts in dense rows |
| `text-xs` | 11.5px | Secondary text, table cells, labels |
| `text-sm` | 12.5px | Buttons, menu items, most controls |
| `text-base` | 13.5px | Body, message content |
| `text-md` | 15px | Card and dialog titles |
| `text-lg`–`4xl` | 17–39px | Page titles and display |

Two families: `sans` for everything, `mono` for anything a human might copy
verbatim — ids, ticket numbers, keys, payloads, timers.

**Tabular figures are mandatory** anywhere numbers stack: tables, metrics,
countdowns, counts. Add `tabular` or use a `numeric` column. Proportional digits
in a column of SLA timers make them impossible to scan.

Font features are set globally: slashed zero and disambiguated `l`, because
agents read identifiers all day and `rn` vs `m` is a real support incident.

## 4. Spacing and density

4px grid. Half-steps only below 8px.

Two density modes (`comfortable`, `compact`) are driven entirely by
`[data-density]` on the root. Density changes **spacing only** — never colour,
weight, or affordance. Control heights are fixed across densities because hit
targets are an accessibility floor, not a preference.

## 5. Elevation

A raised surface in this system is always three things together:

```
fill + 1px hairline + 1px inner top highlight
```

Use the `hc-raised` utility so they never drift apart. On a dark surface, depth
reads from the light top edge more than from shadow — which is why the light
theme sets `--hc-highlight: none` and leans on shadow instead.

Borders are hairlines. If a border is visible from across the room it is doing
too much: structure should come from spacing first, tone second.

## 6. Motion

| Duration | Used for |
|---|---|
| `--dur-fast` (110ms) | Anything triggered dozens of times an hour: menus, rows, tabs |
| `--dur-base` (160ms) | State changes, panel content |
| `--dur-slow` (240ms) | Sheets, drawers, bars that grow |

`prefers-reduced-motion` collapses movement globally while preserving opacity
changes, so state transitions stay perceivable.

**Only one thing in the product pulses: a breaching SLA.** If everything
animates, nothing is urgent.

## 7. Component rules

### Buttons

One `primary` per view. `secondary` for everything else that commits.
`ghost` for toolbar and row actions. `danger` for destructive and irreversible.

`asChild` delegates rendering to a child (usually a `<Link>`). Icons still work
because children are wrapped in `Slottable`.

### Switch vs Checkbox

This is semantic, not stylistic:

- **Switch** — commits immediately. Settings that persist on toggle.
- **Checkbox** — waits for a Save. Form fields.

### Tooltip vs HoverCard vs Callout

- **Tooltip** names a thing. A short phrase plus an optional shortcut.
- **HoverCard** previews an entity without navigating. Never the only path to an
  action — hover is not an affordance on touch.
- **Callout** explains a *situation* that persists on the page: a disabled
  webhook, a paused SLA, a pending domain verification.
- **Toast** is transient and global. One inline action, never more.

### Dialog vs Sheet vs route

- **Dialog** — a decision or a short form.
- **Sheet** — editing something while its context stays visible.
- **Route** — if the user must leave the context anyway.

### Empty states

Say what the thing is *for*, not that it is empty. "No conversations yet" is
useless; "New conversations appear here the moment they arrive" tells someone
whether to wait or to go configure something.

## 8. Domain colour mapping

Conversation states, ticket statuses, priority, and SLA states are mapped to
tones in **one place** — `components/Badge.tsx`. Every surface reads that
mapping, so a "pending" thread is the same colour in the inbox, the ticket
table, a report, and the customer's portal.

Priority is encoded by **bar count first, colour second**, so it survives
greyscale, colour-blindness, and a 200ms glance.

## 9. Accessibility

Not a checklist item; several of these are load-bearing for the product.

- **Keyboard-first.** The inbox is fully operable without a mouse (`j`/`k`,
  `g`-chords, `⌘K`). Shortcuts are surfaced in menus and tooltips, not hidden in
  a help page.
- **One focus ring**, defined once in `base.css`. Never re-declare an outline.
- **Every icon-only control has an `aria-label`.** `IconButton` makes it
  mandatory in the type signature.
- **Live regions** for incoming messages and typing indicators.
- **Reduced motion** respected globally.
- **Forced-colors** mode keeps a visible focus outline.

## 10. Adding a component

1. Does an existing one cover it with a prop? Prefer that.
2. Is it used on more than one screen? If not, keep it local to the page.
3. Semantic tokens only.
4. Keyboard and screen-reader behaviour before visual polish.
5. A comment explaining *why*, where the reasoning is not obvious from the code.

The library is intentionally small. Every component is one more thing that must
stay consistent across four surfaces and two themes.
