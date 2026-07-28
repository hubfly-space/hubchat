//go:build integration

package inbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/workspace"
)

func newTestService(t *testing.T, pool *database.Pool) *inbox.Service {
	t.Helper()
	return inbox.New(pool, events.New(pool), audit.New(pool))
}

// seedWorkspace stands in for what a real sign-up + Bootstrap call produces:
// one workspace with an owner and its default inbox, without going through
// the auth module.
func seedWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) (workspaceID, ownerMemberID string) {
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

func TestCreateInboxIsScopedToItsWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	workspaceA, memberA := seedWorkspace(t, ctx, pool)
	workspaceB, _ := seedWorkspace(t, ctx, pool)

	created, err := svc.Create(ctx, workspaceA, memberA, "Sales", "sales", nil, []string{"widget", "email"}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.Get(ctx, workspaceB, created.ID); !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("cross-workspace get: got %v, want ErrNotFound", err)
	}
	if got, err := svc.Get(ctx, workspaceA, created.ID); err != nil || got.Name != "Sales" {
		t.Fatalf("same-workspace get: got %+v, %v", got, err)
	}
}

func TestCreateInboxRejectsDuplicateSlugAndUnknownChannel(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	workspaceID, memberID := seedWorkspace(t, ctx, pool)

	if _, err := svc.Create(ctx, workspaceID, memberID, "Sales", "sales", nil, nil, nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.Create(ctx, workspaceID, memberID, "Sales Again", "sales", nil, nil, nil); !errors.Is(err, inbox.ErrSlugTaken) {
		t.Fatalf("duplicate slug: got %v, want ErrSlugTaken", err)
	}
	if _, err := svc.Create(ctx, workspaceID, memberID, "Bad", "bad-channel", nil, []string{"carrier-pigeon"}, nil); !errors.Is(err, inbox.ErrInvalidChannel) {
		t.Fatalf("bad channel: got %v, want ErrInvalidChannel", err)
	}
}

func TestDeleteInboxRefusesTheLastOne(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	workspaceID, memberID := seedWorkspace(t, ctx, pool)

	inboxes, err := svc.List(ctx, workspaceID)
	if err != nil || len(inboxes) != 1 {
		t.Fatalf("expected exactly the default inbox, got %+v, %v", inboxes, err)
	}

	if err := svc.Delete(ctx, workspaceID, memberID, inboxes[0].ID); !errors.Is(err, inbox.ErrLastInbox) {
		t.Fatalf("delete last inbox: got %v, want ErrLastInbox", err)
	}
}

// A second inbox can be deleted, and if it was the default, another inbox
// picks up the flag — a workspace must never end up with none.
func TestDeleteInboxPromotesANewDefault(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	workspaceID, memberID := seedWorkspace(t, ctx, pool)

	inboxes, _ := svc.List(ctx, workspaceID)
	original := inboxes[0]

	second, err := svc.Create(ctx, workspaceID, memberID, "Sales", "sales", nil, nil, nil)
	if err != nil {
		t.Fatalf("create second inbox: %v", err)
	}
	if err := svc.SetDefault(ctx, workspaceID, memberID, second.ID); err != nil {
		t.Fatalf("set default: %v", err)
	}

	if err := svc.Delete(ctx, workspaceID, memberID, second.ID); err != nil {
		t.Fatalf("delete new default: %v", err)
	}

	remaining, err := svc.Get(ctx, workspaceID, original.ID)
	if err != nil {
		t.Fatalf("get remaining inbox: %v", err)
	}
	if !remaining.IsDefault {
		t.Fatalf("expected the surviving inbox to become default")
	}
}

func TestSetTeamsReplacesInboxTeamMembership(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	workspaceID, memberID := seedWorkspace(t, ctx, pool)

	teamAID := ids.New(ids.PrefixTeam)
	teamBID := ids.New(ids.PrefixTeam)
	if _, err := pool.Exec(ctx, `
		INSERT INTO teams (id, workspace_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')
	`, teamAID, teamBID, workspaceID); err != nil {
		t.Fatalf("seed teams: %v", err)
	}

	created, err := svc.Create(ctx, workspaceID, memberID, "Sales", "sales", nil, nil, []string{teamAID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(created.TeamIDs) != 1 || created.TeamIDs[0] != teamAID {
		t.Fatalf("expected team A on create, got %v", created.TeamIDs)
	}

	updated, err := svc.Update(ctx, workspaceID, memberID, created.ID, "Sales", nil, nil, []string{teamBID})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.TeamIDs) != 1 || updated.TeamIDs[0] != teamBID {
		t.Fatalf("expected team B after update, got %v", updated.TeamIDs)
	}
}
