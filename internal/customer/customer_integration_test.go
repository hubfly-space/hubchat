//go:build integration

package customer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func newTestService(t *testing.T, pool *database.Pool) *customer.Service {
	t.Helper()
	return customer.New(pool, events.New(pool), audit.New(pool))
}

func seedWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) (workspaceID, memberID string) {
	t.Helper()

	wsSvc := workspace.New(pool, events.New(pool), audit.New(pool))

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, 'Test Owner', $2, 'x', now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	token := ids.New("t")
	ws, err := wsSvc.Bootstrap(ctx, userID, "Acme", "acme-"+token[len(token)-10:])
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	actor, err := wsSvc.ActorForUser(ctx, ws.ID, userID)
	if err != nil {
		t.Fatalf("resolve owner actor: %v", err)
	}
	return ws.ID, actor.MemberID
}

func seedCustomer(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID, name string) string {
	t.Helper()
	id := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `
		INSERT INTO customers (id, workspace_id, name) VALUES ($1, $2, $3)
	`, id, workspaceID, name); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return id
}

func seedTag(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID, name string) string {
	t.Helper()
	id := ids.New(ids.PrefixTag)
	if _, err := pool.Exec(ctx, `INSERT INTO tags (id, workspace_id, name) VALUES ($1, $2, $3)`, id, workspaceID, name); err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	return id
}

func TestGetScopesToWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsA, _ := seedWorkspace(t, ctx, pool)
	wsB, _ := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsA, "Ada")

	if _, err := svc.Get(ctx, wsB, cust); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("cross-workspace get: got %v, want ErrNotFound", err)
	}
	got, err := svc.Get(ctx, wsA, cust)
	if err != nil || got.Name == nil || *got.Name != "Ada" {
		t.Fatalf("same-workspace get: got %+v, %v", got, err)
	}
}

func TestUpdateRejectsAStaleVersion(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, memberID := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsID, "Ada")

	name := "Ada Lovelace"
	updated, err := svc.Update(ctx, wsID, memberID, cust, 1, &name, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version to bump to 2, got %d", updated.Version)
	}

	// Retrying with the now-stale version 1 must fail rather than clobber it.
	otherName := "Someone Else"
	if _, err := svc.Update(ctx, wsID, memberID, cust, 1, &otherName, nil, nil, nil, nil); !errors.Is(err, customer.ErrVersionConflict) {
		t.Fatalf("stale update: got %v, want ErrVersionConflict", err)
	}

	reloaded, err := svc.Get(ctx, wsID, cust)
	if err != nil || reloaded.Name == nil || *reloaded.Name != "Ada Lovelace" {
		t.Fatalf("expected the stale write to be rejected, got %+v, %v", reloaded, err)
	}
}

func TestAddTagRejectsATagFromAnotherWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsA, memberA := seedWorkspace(t, ctx, pool)
	wsB, _ := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsA, "Ada")

	tagB := seedTag(t, ctx, pool, wsB, "foreign")
	if err := svc.AddTag(ctx, wsA, memberA, cust, tagB); !errors.Is(err, customer.ErrTagNotFound) {
		t.Fatalf("cross-workspace tag: got %v, want ErrTagNotFound", err)
	}

	tagA := seedTag(t, ctx, pool, wsA, "vip")
	if err := svc.AddTag(ctx, wsA, memberA, cust, tagA); err != nil {
		t.Fatalf("same-workspace tag: %v", err)
	}
	tags, err := svc.Tags(ctx, wsA, cust)
	if err != nil || len(tags) != 1 || tags[0] != tagA {
		t.Fatalf("expected exactly [vip], got %v, %v", tags, err)
	}

	if err := svc.RemoveTag(ctx, wsA, memberA, cust, tagA); err != nil {
		t.Fatalf("remove tag: %v", err)
	}
	tags, err = svc.Tags(ctx, wsA, cust)
	if err != nil || len(tags) != 0 {
		t.Fatalf("expected no tags after removal, got %v, %v", tags, err)
	}
}

func TestSearchMatchesNameCaseInsensitively(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, _ := seedWorkspace(t, ctx, pool)
	seedCustomer(t, ctx, pool, wsID, "Grace Hopper")
	seedCustomer(t, ctx, pool, wsID, "Ada Lovelace")

	results, err := svc.Search(ctx, wsID, "grace", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].Name == nil || *results[0].Name != "Grace Hopper" {
		t.Fatalf("expected exactly Grace Hopper, got %+v", results)
	}
}

func TestBlockIsIdempotentAndRejectsUnknownKind(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, memberID := seedWorkspace(t, ctx, pool)

	if err := svc.Block(ctx, wsID, memberID, "not-a-kind", "x", nil); !errors.Is(err, customer.ErrInvalidBlockKind) {
		t.Fatalf("invalid kind: got %v, want ErrInvalidBlockKind", err)
	}

	if err := svc.Block(ctx, wsID, memberID, "email", "spammer@example.com", nil); err != nil {
		t.Fatalf("first block: %v", err)
	}
	// Blocking the same contact again (e.g. a second complaint) must update
	// rather than fail on the unique (workspace_id, kind, value) constraint.
	reason := "repeat abuse"
	if err := svc.Block(ctx, wsID, memberID, "email", "spammer@example.com", &reason); err != nil {
		t.Fatalf("re-block: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM blocked_contacts WHERE workspace_id = $1`, wsID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one blocked_contacts row, got %d", count)
	}
}
