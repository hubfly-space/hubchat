package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
)

// migrationLockKey is an arbitrary constant used as the advisory lock key for
// migration application. Any int64 works as long as no other subsystem in
// this codebase reuses it; it is namespaced here so that stays easy to audit.
const migrationLockKey = 8_412_001

// EnsureMigrationsTable creates the bookkeeping table if it does not exist.
// Separate from Migrate so `migrate status` can inspect state without holding
// the advisory lock that applying migrations requires.
func EnsureMigrationsTable(ctx context.Context, pool *Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename    text PRIMARY KEY,
			applied_at  timestamptz NOT NULL DEFAULT now()
		)
	`)
	return err
}

// AppliedMigrations returns the filenames already recorded as applied.
func AppliedMigrations(ctx context.Context, pool *Pool) (map[string]bool, error) {
	if err := EnsureMigrationsTable(ctx, pool); err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

// Migrate applies every embedded migration not yet recorded, in filename
// order, under a session-level advisory lock.
//
// The lock is what makes this safe to call from every instance at boot: only
// one holder applies migrations at a time, and the rest either wait briefly
// or (more commonly, since the lock is fast to acquire and release) find
// nothing left to do by the time they get it (§18 migration locking).
func Migrate(ctx context.Context, pool *Pool, migrations fs.FS) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("database: acquire connection for migration lock: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return nil, fmt.Errorf("database: acquire migration lock: %w", err)
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockKey) //nolint:errcheck

	if err := EnsureMigrationsTable(ctx, pool); err != nil {
		return nil, err
	}

	applied, err := AppliedMigrations(ctx, pool)
	if err != nil {
		return nil, err
	}

	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		return nil, fmt.Errorf("database: read embedded migrations: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	var newlyApplied []string
	for _, name := range names {
		if applied[name] {
			continue
		}

		body, err := fs.ReadFile(migrations, name)
		if err != nil {
			return newlyApplied, fmt.Errorf("database: read migration %s: %w", name, err)
		}

		// Each migration is its own transaction. Files already wrap themselves
		// in BEGIN/COMMIT for clarity when read standalone, so run the body as
		// a single batched exec rather than nesting a second transaction.
		if _, err := conn.Exec(ctx, string(body)); err != nil {
			return newlyApplied, fmt.Errorf("database: apply migration %s: %w", name, err)
		}

		if _, err := conn.Exec(ctx,
			`INSERT INTO schema_migrations (filename) VALUES ($1)`, name,
		); err != nil {
			return newlyApplied, fmt.Errorf("database: record migration %s: %w", name, err)
		}

		newlyApplied = append(newlyApplied, name)
	}

	return newlyApplied, nil
}

// ErrPendingMigrations is returned by VerifyNoPending when migrations exist
// that have not been applied — the "verify" policy from config.MigratePolicy.
var ErrPendingMigrations = errors.New("pending migrations exist; run `hubchat migrate` or set HUBCHAT_MIGRATE=apply")

// VerifyNoPending fails startup rather than silently running with a schema
// older than the binary expects (§8.8 migration policy default).
func VerifyNoPending(ctx context.Context, pool *Pool, migrations fs.FS) error {
	applied, err := AppliedMigrations(ctx, pool)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		return fmt.Errorf("database: read embedded migrations: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !applied[entry.Name()] {
			return fmt.Errorf("%w (missing: %s)", ErrPendingMigrations, entry.Name())
		}
	}
	return nil
}

// WithTx runs fn inside a transaction, committing on success and rolling back
// on error or panic. The one transaction helper every service method uses
// (docs/backend.md §4).
func WithTx(ctx context.Context, pool *Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("database: begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database: commit transaction: %w", err)
	}
	return nil
}
