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

After building and starting a production binary, run the live HTTP smoke
check with:

```bash
HUBCHAT_SMOKE_BASE_URL="http://127.0.0.1:8080" make production-http-check
```

This verifies the binary's health/readiness endpoints, all three embedded
browser surfaces, and the live API route.

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

## Browser journeys

The cross-module API journey currently runs against a real PostgreSQL database:

```bash
HUBCHAT_TEST_DATABASE_URL="postgres://hubchat:hubchat@127.0.0.1:5432/hubchat_test?sslmode=disable" \
  go test ./internal/api -tags=integration -run '^TestCoreSupportPortalJourney$' -count=1
```

It covers workspace bootstrap, customer identity, portal magic-link login,
ticket creation, agent and customer replies, attachment upload/download, and
idempotent reply retry. A browser-level acceptance suite should still run
against a built binary with a temporary PostgreSQL database. The full journey
set is documented in
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
