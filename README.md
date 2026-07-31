# Hubchat

Open-source customer support: live chat, ticketing, customer portals, feedback
boards, knowledge base, and customer context — in one Go binary.

No Node.js runtime in production. No Redis. No message broker. No AI.

```
PostgreSQL  ──▶  hubchat (one binary)  ──▶  browsers
                 · HTTP + REST API
                 · WebSocket gateway
                 · background worker + scheduler
                 · embedded dashboard, portal, and widget
```

---

## Status

This repository contains a working production-backed support core alongside
fixture-driven screens that are being migrated module by module. Authentication,
workspaces, inboxes, conversations, realtime messaging, customers, companies,
tickets, metadata, search, widget identity, audit logging, jobs, and migrations
are implemented. Feedback, knowledge base, surveys, forms, automation, SLA,
email ingestion, webhooks, analytics, files, and portability remain in active
delivery.

What runs today:

| | |
|---|---|
| `make build` | Produces a single ~18 MB binary with all three frontends embedded |
| `./dist/hubchat serve` | Serves the dashboard, portal, and widget with correct cache and security headers |
| `./dist/hubchat doctor` | Checks configuration, embedded assets, and dependencies |
| `make check` | Typecheck, lint, vet, and unit test |

Integration tests use a separate destructive test database and run with
`make test-integration` after `make dev-db` and `export HUBCHAT_TEST_DATABASE_URL=...`.

---

## Quick start

```bash
# Requirements: Go 1.25+, Node 22+, pnpm, PostgreSQL 15+
make install

createdb hubchat
export HUBCHAT_DATABASE_URL="postgres://localhost:5432/hubchat"
export HUBCHAT_PUBLIC_URL="http://localhost:8080"
export HUBCHAT_SECRET_KEY="$(openssl rand -base64 32)"

make build
./dist/hubchat migrate
./dist/hubchat serve
```

Then open <http://localhost:8080/app/>.

### Working on the interface

```bash
make dev          # Go on :8080, dashboard on :5173 with hot reload
make dev-portal   # customer portal on :5174
make dev-widget   # widget harness on :5175
```

The widget harness deliberately loads the widget into a hostile host page —
serif type, oversized pink buttons, an `!important` border reset — so shadow-root
isolation failures are visible immediately rather than in a customer's site.

---

## Layout

```
cmd/hubchat/          CLI entry point
internal/             one package per domain module (§8.4), each with doc.go
  httpserver/         routing, middleware, asset serving, error contract
  config/             configuration loading and validation
  <26 modules>/       domain services, repositories, and boundary contracts
embedded/             go:embed surface — assets, migrations, templates
  migrations/         SQL, applied in filename order
web/
  shared/             design system: tokens, components, domain types
  dashboard/          agent inbox + admin (≈70 routes)
  portal/             customer-facing portal
  widget/             embeddable widget + loader
docs/                 architecture and engineering guidelines
```

## Documentation

| Document | What it covers |
|---|---|
| [Architecture](docs/architecture.md) | How the pieces fit, and the decisions behind the shape |
| [Design system](docs/design-system.md) | Tokens, colour discipline, component rules |
| [Frontend guidelines](docs/frontend.md) | React conventions, state, performance budgets |
| [Backend guidelines](docs/backend.md) | Module boundaries, database rules, error handling |
| [API conventions](docs/api-conventions.md) | REST shape, pagination, idempotency, webhooks |
| [Security](docs/security.md) | Tenant isolation, identity verification, the threat model |
| [Contributing](CONTRIBUTING.md) | How to propose and land a change |
| [ADRs](docs/adr/) | Decisions that would otherwise be re-argued |

---

## Two things worth knowing up front

**Tenant isolation is the security boundary.** Every query is scoped by
workspace. A missing workspace predicate is treated as a critical defect, not a
bug — see [docs/security.md](docs/security.md).

**There are no AI features, and this is a product decision rather than a
roadmap gap.** Productivity comes from fast search, macros, saved replies,
deterministic rules, structured metadata, and interface design. A support answer
should be traceable to a person or a rule.

## Licence

Not yet chosen. It has to be settled before the first release, along with which
components are open and whether hosted-only extensions may exist — tracked in
[docs/adr/README.md](docs/adr/README.md#still-open).
