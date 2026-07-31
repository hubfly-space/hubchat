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

## PostgreSQL integration tests

Start the development dependencies, then point the test suite at a dedicated
database. Do not use a database containing local work.

```bash
make dev-db
export HUBCHAT_TEST_DATABASE_URL="postgres://hubchat:hubchat@localhost:5432/hubchat?sslmode=disable"
make test-integration
```

The suite runs one package at a time because each package resets the shared
test database. A missing `HUBCHAT_TEST_DATABASE_URL` skips integration tests by
design; it never guesses which database is safe to destroy.

## Browser journeys

The browser acceptance suite should run against a built binary with a temporary
PostgreSQL database. The minimum journey set is documented in
`.material/idea.md`: setup, widget conversation, verified identity, agent
reply, ticket/portal reply, attachments, feedback, knowledge-base search,
surveys, SLA/automation, webhook replay, and workspace export/import.
