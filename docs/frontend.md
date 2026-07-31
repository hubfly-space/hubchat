# Frontend guidelines

React, TypeScript, Tailwind v4, three bundles, one design system.

---

## 1. Where code goes

```
web/shared/          used by ≥2 surfaces
  src/styles/        the token layer
  src/components/    the library
  src/lib/           cn, formatting, hooks, theme
  src/types/         the API contract mirror
web/dashboard/src/
  app/               shell, routing, workspace context
  pages/<domain>/    one directory per domain
  data/              explicit demo fixtures only (production pages use API/query modules)
  components/        dashboard-only components
```

**A component belongs in `shared/` only when a second surface imports it.** One
speculative move into shared costs every surface its bundle weight.

## 2. Types are the contract

`web/shared/src/types/` mirrors the v1 API. It is hand-maintained today and will
become generated output from the OpenAPI document.

Until then: **change a type here and the corresponding Go struct in the same
commit.** Conventions match the wire format — `snake_case` fields, opaque
prefixed ids, RFC 3339 timestamps — so payloads round-trip untouched and nobody
writes a mapping layer.

Never parse an id. `cnv_01hq7x…` is opaque; the prefix is for humans reading
logs.

## 3. State

Four kinds, four homes:

| Kind | Where | Example |
|---|---|---|
| Server data | Query cache | Conversations, tickets |
| URL state | Route params and search | Active view, filters, selected record |
| Session state | Context | Workspace, viewer, capabilities |
| Ephemeral UI | `useState` | Dialog open, draft text |

**Filters and selection belong in the URL.** An agent must be able to send a
colleague a link to exactly what they are looking at. This is why the inbox
route is `/inbox/:viewId/:conversationId` and not two pieces of component state.

No global store. The things that genuinely need to be global — workspace,
viewer, theme — are three small contexts.

## 4. Permissions in the UI

```tsx
{can("conversation.delete") && <DeleteButton />}
```

`can()` decides whether to *render* a control. It is not a security check. Every
mutation is re-checked in the Go service layer, and hiding a control the server
would reject is a courtesy, not a boundary. Never rely on it for anything that
matters. See [security.md](security.md).

## 5. Performance

### Budgets

| Bundle | Initial JS (gzip) | Enforced by |
|---|---|---|
| Widget loader | ~5 KB | Hand-written, no framework |
| Widget interface | < 70 KB | `chunkSizeWarningLimit` |
| Portal entry | < 30 KB | Route splitting + `chunkSizeWarningLimit` |
| Dashboard entry | < 60 KB | Route splitting |

Vendor code is split into long-lived `vendor-react` and `vendor-ui` chunks so
shipping a feature does not invalidate React in everyone's cache.

Two traps worth naming, because both were live in this repository:

- **`manualChunks` by bare specifier misses subpath imports.** Listing
  `"react-dom"` does not catch `react-dom/client`, which is what the app
  actually imports — leaving the largest dependency in the app chunk. Match on
  resolved path.
- **A barrel export defeats tree-shaking unless the package declares
  `sideEffects`.** `web/shared` is marked side-effect-free (except CSS), which
  is what lets the portal import from `@hubchat/shared` and still drop the ~30
  components it does not use.

### Lists

Virtualise anything unbounded. Rows must be height-stable and keyed.

Realtime updates patch the cache; they never refetch the list. A thousand-row
inbox that refetches on every incoming message is a thousand-row inbox that is
always loading.

### Timers

One ticking clock per component, passed down — not one per row. `useNow()`
exists for this. A conversation list with a timer per row schedules a thousand
intervals.

## 6. Forms

- `Field` wires label, description, and error to the control via
  `aria-describedby`.
- Validate on blur, not on keystroke. Validating as someone types tells them
  their half-typed email is invalid, which they know.
- `description` is guidance shown before the user acts. `error` replaces it.
  Never use `description` for errors.
- Destructive confirmations state the **outcome**, not the question. "42
  conversations will be anonymised" beats "Are you sure?".

## 7. Styling

Tailwind utilities, semantic tokens, `cn()` for composition.

```tsx
// yes
<div className="bg-surface border border-line text-fg-muted" />

// no — hard-codes the theme
<div className="bg-[#131519] border-[#232326]" />
```

`cn()` is `clsx` plus a `tailwind-merge` instance taught our custom scales, so
`text-md text-lg` resolves correctly at the call site.

Arbitrary values are acceptable for genuine one-offs (`w-[372px]`), not for
colour.

## 8. Comments

Comment the **why**, never the what.

```tsx
// Client-generated so the send is idempotent if the socket retries (§9).
const id = `cli_${Date.now().toString(36)}`;
```

Comment when: the code encodes a product or security decision; a simpler
approach was rejected for a reason; something looks wrong but is not.

Do not comment: what a well-named function does, obvious JSX, or anything the
type signature already says.

## 9. Checklist before opening a PR

- [ ] `pnpm typecheck` and `pnpm lint` pass
- [ ] Keyboard-operable; icon-only controls have `aria-label`
- [ ] Works in both themes and both densities
- [ ] Loading, empty, and error states exist
- [ ] No hard-coded colours
- [ ] No new dependency without a note on why
- [ ] Bundle budgets still met
