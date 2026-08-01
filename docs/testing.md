# Testing Hubchat

Hubchat has fast checks that do not require infrastructure and an integration
suite that deliberately uses a separate PostgreSQL database. The integration
database is destructive: tests delete workspace-owned rows between cases.

## Fast checks

```bash
pnpm -r --filter "./web/*" typecheck
pnpm -r --filter "./web/*" lint
pnpm -r --filter "./web/*" build
go vet ./...
go test ./... -race -count=1
```

`make check` runs the same release checks.

The production HTTP router smoke test also runs in the fast Go suite. It
proves the compiled server serves health endpoints, the three embedded browser
surfaces, the root redirect, and a safe no-database API response.
The same package includes a bounded concurrent smoke test for health and
widget asset requests; it is a transport sanity check, not a substitute for
production-scale capacity testing.
The PostgreSQL conversation integration suite also exercises a 160-conversation
inbox with concurrent paginated reads and verifies workspace/inbox isolation.
The realtime integration suite exercises a six-client, 80-event workspace
burst and verifies every client receives ordered, gap-free frames.
The HTTP server package also starts on a dynamically reserved port, serves a
health request, and verifies clean context-driven shutdown.

For a data-heavy PostgreSQL acceptance gate, run:

```bash
make test-capacity
```

This opt-in test resets the dedicated test database, seeds a production-shaped
inbox with 25,000 conversations and messages plus a second workspace, then
performs concurrent indexed inbox reads. It verifies page size, tenant
isolation, and p50/p95/p99 latency. Tune `HUBCHAT_SCALE_CONVERSATIONS`,
`HUBCHAT_SCALE_WORKERS`, `HUBCHAT_SCALE_REQUESTS`, and
`HUBCHAT_SCALE_MAX_P95_MS` for the installation's data volume and service
objective. This validates one PostgreSQL deployment shape; multi-node capacity
still requires running the same gate against the planned topology.

The optional provider adapters can be exercised against the local MinIO and
MailHog services with:

```bash
make dev-db
make provider-check
```

The provider gate uploads, reads, and deletes an object through the real
S3-compatible endpoint, then sends an SMTP message and verifies it through the
MailHog inspection API. Override `HUBCHAT_PROVIDER_S3_*`,
`HUBCHAT_PROVIDER_SMTP_*`, and `HUBCHAT_PROVIDER_SMTP_INSPECTION_URL` for an
isolated staging provider. It is never part of `make check` because external
provider availability is optional by design.

After building and starting a production binary, run the live HTTP smoke
check with:

```bash
HUBCHAT_SMOKE_BASE_URL="http://127.0.0.1:8080" make production-http-check
```

This verifies the binary's health/readiness endpoints, all three embedded
browser surfaces, and the live API route.

For a bounded production-load baseline, run the binary smoke first and then:

```bash
HUBCHAT_LOAD_BASE_URL="http://127.0.0.1:8080" \
HUBCHAT_LOAD_DURATION_MS=10000 \
HUBCHAT_LOAD_CONCURRENCY=32 \
make production-load-check
```

The load check cycles through health, readiness, embedded dashboard/portal/
widget assets, and the live metadata route. It reports status counts and p50,
p95, and p99 latency, fails on any unexpected response or timeout, accepts the
metadata route's intentional `429` rate-limit response, and is bounded by the
configured duration. It is a deployment smoke baseline, not a substitute for
a capacity test at the target installation's expected scale.

To build and validate the exact production binary without manually starting a
server, use a dedicated PostgreSQL database:

```bash
HUBCHAT_BINARY_DATABASE_URL="postgres://hubchat:hubchat@127.0.0.1:5432/hubchat_test?sslmode=disable" \
  make production-binary-check
```

This target builds the embedded release artifact, starts the documented
`http,realtime` process split with `HUBCHAT_DEV=0`
and `HUBCHAT_MIGRATE=verify`, runs both HTTP checks, and requires graceful
shutdown. It also starts the worker/scheduler role split briefly and verifies
that those roles start and stop cleanly. It must not be pointed at a database
containing live work.

## PostgreSQL integration tests

Start the development dependencies, then point the test suite at a dedicated
database. Do not use a database containing local work.

```bash
make dev-db
make test-integration
```

`make test-integration` defaults to `hubchat_test`, a dedicated database. When
the local Compose PostgreSQL service is running, the target creates that
database if necessary; otherwise it assumes the URL already points at an
externally provisioned test database. It migrates and then destroys test data
only — it never touches the development database (`hubchat`). Point it
elsewhere only deliberately:
`export HUBCHAT_TEST_DATABASE_URL="postgres://hubchat:hubchat@127.0.0.1:5432/other?sslmode=disable"`.

The suite runs one package at a time because each package resets the shared
test database. A missing `HUBCHAT_TEST_DATABASE_URL` skips integration tests by
design; it never guesses which database is safe to destroy.

## Production journeys

The production-binary check also runs an HTTP acceptance journey against the
compiled binary. It creates an isolated, uniquely named owner/workspace,
installs a widget, verifies public widget configuration and visitor identity,
creates and publicly bootstraps a portal, starts and replies to a visitor
conversation, configures an SLA calendar and policy, creates and replays a
webhook delivery, publishes and searches a knowledge-base article, records
article helpfulness, creates and answers an anonymous CSAT survey, submits and
votes on widget feedback, converts the conversation to a ticket, dry-runs an
automation rule, uploads and downloads a ticket attachment, and checks
workspace export preview.
Run it directly against an already running binary with:

```bash
HUBCHAT_JOURNEY_BASE_URL="http://127.0.0.1:8080" make production-journey-check
```

This is an API-level journey, not a browser DOM test. It catches production
router, cookie, CSRF, tenant, widget-origin, and cross-module contract errors
without adding a browser runtime to the Go release artifact.

## Browser journeys

The cross-module API journey currently runs against a real PostgreSQL database:

```bash
HUBCHAT_TEST_DATABASE_URL="postgres://hubchat:hubchat@127.0.0.1:5432/hubchat_test?sslmode=disable" \
  go test ./internal/api -tags=integration -run '^TestCoreSupportPortalJourney$' -count=1
```

It covers workspace bootstrap, customer identity, portal magic-link login,
ticket creation, agent and customer replies, attachment upload/download, and
idempotent reply retry. A browser-level acceptance suite should still run
against a built binary with a temporary PostgreSQL database. The production
HTTP journey above covers the operator/widget portion; a browser-level suite
is still useful for DOM, keyboard, accessibility, and real portal navigation.
The full journey set is documented in
`.material/idea.md`: setup, widget conversation, verified identity, agent
reply, ticket/portal reply, attachments, feedback, knowledge-base search,
surveys, SLA/automation, webhook replay, and workspace export/import.

The self-service module composition journey can be run with:

```bash
HUBCHAT_TEST_DATABASE_URL="postgres://hubchat:hubchat@127.0.0.1:5432/hubchat_test?sslmode=disable" \
  go test ./internal/api -tags=integration -run '^TestSelfServiceJourney$' -count=1
```

It covers customer feedback submission, voting, comments, status changes,
published knowledge-base search and helpfulness feedback, survey submission
and aggregation, plus cross-workspace isolation.
