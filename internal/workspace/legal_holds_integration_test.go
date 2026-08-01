//go:build integration

package workspace_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestLegalHoldProtectsRetentionAcrossWorkspaceBoundaries(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	workspaceService := newTestService(t, pool)
	workspaceA, _, memberA := seedOwnerWorkspace(t, ctx, pool, workspaceService, "hold-a@example.com")
	workspaceB, _, memberB := seedOwnerWorkspace(t, ctx, pool, workspaceService, "hold-b@example.com")
	customerA := seedLegalHoldCustomer(t, ctx, pool, workspaceA)
	customerB := seedLegalHoldCustomer(t, ctx, pool, workspaceB)
	setShortRetention(t, ctx, pool, workspaceA)
	setShortRetention(t, ctx, pool, workspaceB)
	seedExpiredRetentionRows(t, ctx, pool, workspaceA, customerA)
	seedExpiredRetentionRows(t, ctx, pool, workspaceB, customerB)

	hold, err := workspaceService.CreateLegalHold(ctx, workspaceA, memberA, workspace.LegalHoldInput{Category: "all", Reason: "Preserve records for incident review"})
	if err != nil {
		t.Fatalf("create legal hold: %v", err)
	}
	if hold.ReleasedAt != nil {
		t.Fatal("new legal hold is already released")
	}

	customerService := customer.New(pool, events.New(pool), audit.New(pool), config.Limits{MaxEventBytes: 32 << 10, MaxAttributesPerCustomer: 100})
	eventsDeleted, sessionsDeleted, err := customerService.RunRetentionSweep(ctx)
	if err != nil {
		t.Fatalf("retention sweep with hold: %v", err)
	}
	if eventsDeleted != 1 || sessionsDeleted != 1 {
		t.Fatalf("expected only workspace B's expired rows to be deleted, got events=%d sessions=%d", eventsDeleted, sessionsDeleted)
	}

	if _, err := workspaceService.ReleaseLegalHold(ctx, workspaceB, memberB, hold.ID); !errors.Is(err, workspace.ErrLegalHoldNotFound) {
		t.Fatalf("foreign workspace release error = %v, want ErrLegalHoldNotFound", err)
	}
	active, err := workspaceService.ListLegalHoldsPage(ctx, workspaceA, false, time.Time{}, "", 10)
	if err != nil || len(active) != 1 || active[0].ID != hold.ID {
		t.Fatalf("active holds = %+v, err=%v", active, err)
	}

	if _, err := workspaceService.ReleaseLegalHold(ctx, workspaceA, memberA, hold.ID); err != nil {
		t.Fatalf("release legal hold: %v", err)
	}
	if _, err := workspaceService.ReleaseLegalHold(ctx, workspaceA, memberA, hold.ID); !errors.Is(err, workspace.ErrLegalHoldNotFound) {
		t.Fatalf("second release error = %v, want ErrLegalHoldNotFound", err)
	}

	eventsDeleted, sessionsDeleted, err = customerService.RunRetentionSweep(ctx)
	if err != nil {
		t.Fatalf("retention sweep after release: %v", err)
	}
	if eventsDeleted != 1 || sessionsDeleted != 1 {
		t.Fatalf("expected workspace A's held rows to delete after release, got events=%d sessions=%d", eventsDeleted, sessionsDeleted)
	}

	history, err := workspaceService.ListLegalHoldsPage(ctx, workspaceA, true, time.Time{}, "", 10)
	if err != nil || len(history) != 1 || history[0].ReleasedAt == nil {
		t.Fatalf("legal hold history = %+v, err=%v", history, err)
	}
}

func seedLegalHoldCustomer(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID string) string {
	t.Helper()
	id := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id, workspace_id, name) VALUES ($1, $2, 'Retention customer')`, id, workspaceID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return id
}

func setShortRetention(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		UPDATE workspaces SET settings = '{"privacy":{"retention_days":{"events":7,"sessions":7}}}'::jsonb
		WHERE id = $1
	`, workspaceID); err != nil {
		t.Fatalf("set retention policy: %v", err)
	}
}

func seedExpiredRetentionRows(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID, customerID string) {
	t.Helper()
	old := ids.New(ids.PrefixCustomerEvent)
	if _, err := pool.Exec(ctx, `
		INSERT INTO customer_events (id, workspace_id, customer_id, type, source, occurred_at)
		VALUES ($1, $2, $3, 'page.viewed', 'rest_api', now() - interval '30 days')
	`, old, workspaceID, customerID); err != nil {
		t.Fatalf("seed old event: %v", err)
	}
	session := ids.New(ids.PrefixContactSession)
	if _, err := pool.Exec(ctx, `
		INSERT INTO contact_sessions (id, workspace_id, customer_id, started_at, last_seen_at, current_url)
		VALUES ($1, $2, $3, now() - interval '30 days', now() - interval '30 days', '/pricing')
	`, session, workspaceID, customerID); err != nil {
		t.Fatalf("seed old session: %v", err)
	}
}
