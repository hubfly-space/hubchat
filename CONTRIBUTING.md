# Contributing

---

## Setup

```bash
# Go 1.25+, Node 22+, pnpm 10+, PostgreSQL 15+
make install
createdb hubchat

export HUBCHAT_DATABASE_URL="postgres://localhost:5432/hubchat"
export HUBCHAT_PUBLIC_URL="http://localhost:8080"
export HUBCHAT_SECRET_KEY="$(openssl rand -base64 32)"

make dev     # Go on :8080, dashboard on :5173
```

`make help` lists everything.

## Before you open a PR

```bash
make check   # typecheck, lint, vet, test
# Dedicated PostgreSQL suite; see docs/testing.md
make dev-db
make test-integration   # runs against hubchat_test, never your dev database
```

## Commits

Conventional commits, scoped by module:

```
feat(conversation): add snooze-until-customer-replies
fix(widget): stop launcher inheriting host page font
docs(security): document identity-merge rules
```

Explain **why** in the body when the reason is not obvious from the diff. The
diff already says what changed.

## What gets a PR rejected

- A query without a workspace predicate.
- Business logic in an HTTP handler.
- A cross-module database read.
- Hard-coded colours instead of semantic tokens.
- A new dependency without a note on why an existing one will not do.
- A destructive action whose confirmation asks "Are you sure?" rather than
  stating the outcome.
- **An AI feature.** This is a product decision, not an oversight — see
  [ADR-0003](docs/adr/0003-no-ai-features.md).

## Adding a module

1. `internal/<name>/doc.go` stating responsibilities and boundary.
2. `service.go` — business logic, taking a context and returning domain errors.
3. `repository.go` — SQL, workspace-scoped.
4. `handler.go` in `httpserver` — decode, authorize, call, encode.
5. Migration in `embedded/migrations/`, additive.
6. A cross-tenant access test.

## Adding a migration

- Filename: `NNNN_short_description.sql`, next in sequence.
- **Never edit a migration that has shipped.** Supersede it.
- Additive first: add, backfill, switch reads, drop later.
- `CREATE INDEX CONCURRENTLY` on populated tables, outside a transaction.
- Comment any non-obvious index with the query it exists for.

## Adding a component

See [docs/design-system.md](docs/design-system.md#10-adding-a-component).
Shortest version: check whether an existing component covers it first, and keep
it local unless a second surface needs it.

## Architecture decisions

Anything that would otherwise be re-argued in six months gets an ADR in
`docs/adr/`. Copy the format of an existing one. An ADR is cheap; re-litigating
a decision from memory is not.

## Code of conduct

Be straightforward and assume good faith. Review the code, not the person.
Report problems to `conduct@hubchat.dev`.
