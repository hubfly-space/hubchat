// Package dbtest provides a real PostgreSQL connection for integration tests.
//
// docs/backend.md §9 requires migrations, tenant scoping, transactions, job
// leasing, advisory locks, full-text search, and idempotency to be tested
// against a real database rather than a mock. Every one of those is a
// behaviour PostgreSQL provides and a fake would have to reimplement — a mock
// that returns what we expect proves only that we agree with ourselves.
//
// Tests using this package are gated behind the `integration` build tag, so
// `make test` stays runnable without a database and `make test-integration`
// exercises the real thing.
package dbtest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/database"
)

// URLEnv names the environment variable holding the test database URL.
//
// Separate from HUBCHAT_DATABASE_URL so running the suite can never point at a
// development database by accident — opting in has to be deliberate, because
// these tests truncate tables.
const URLEnv = "HUBCHAT_TEST_DATABASE_URL"

// Pool returns a pool connected to the test database, skipping the test when
// one is not configured.
//
// Skipping rather than failing is deliberate: a contributor without Docker
// running should get a clear "not configured" rather than a red suite that
// tells them nothing about their change.
func Pool(t *testing.T) *database.Pool {
	t.Helper()

	url := os.Getenv(URLEnv)
	if url == "" {
		t.Skipf("%s is not set; run `make dev-db` and export it to run integration tests", URLEnv)
	}

	cfg := config.Default().Database
	cfg.URL = url

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// Reset empties every tenant-owned table, leaving the schema and the global
// role seed intact.
//
// Because this clears the whole database rather than one test's fixtures, the
// integration suite must run one package at a time — `make test-integration`
// passes -p 1 for exactly this reason. Two packages resetting concurrently
// delete each other's workspaces mid-test, which surfaces as foreign key
// violations and deadlocks that look like product bugs.
//
// It deletes from `workspaces` and lets ON DELETE CASCADE do the rest, rather
// than TRUNCATE ... CASCADE. That is the exact hazard migration 0003
// documents: TRUNCATE has no WHERE clause, so cascading from `workspaces`
// would empty `roles` and `role_permissions` too — including the built-in rows
// that belong to no workspace — and every later test would run against a
// database with no capability model.
func Reset(t *testing.T, pool *database.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Users are not workspace-owned, so they need deleting separately.
	// Everything else hangs off one of these two by foreign key.
	for _, stmt := range []string{
		`DELETE FROM workspaces`,
		`DELETE FROM users`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("reset test database (%s): %v", stmt, err)
		}
	}
}

// Context returns a context with a deadline suitable for one test.
func Context(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}
