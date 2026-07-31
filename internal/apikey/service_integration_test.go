//go:build integration

package apikey

import (
	"context"
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestRotateAtomicallyRevokesOldKeyAndPreservesWorkspaceScope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	workspaceID, memberID := seedAPIKeyWorkspace(t, ctx, pool)
	service := New(pool)
	created, err := service.Create(ctx, workspaceID, memberID, "production", []string{"conversation.read"}, nil)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	rotated, err := service.Rotate(ctx, workspaceID, memberID, created.ID, "", nil, nil)
	if err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	if rotated.Token == created.Token || rotated.ID == created.ID {
		t.Fatal("rotation reused the original credential")
	}
	if _, err := service.Authenticate(ctx, created.Token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old token authentication error = %v, want ErrInvalidToken", err)
	}
	principal, err := service.Authenticate(ctx, rotated.Token)
	if err != nil {
		t.Fatalf("new token authentication: %v", err)
	}
	if principal.WorkspaceID != workspaceID {
		t.Fatalf("new token workspace = %q, want %q", principal.WorkspaceID, workspaceID)
	}
	if _, err := service.Rotate(ctx, "workspace-from-another-tenant", memberID, rotated.ID, "", nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace rotation error = %v, want ErrNotFound", err)
	}
	if _, err := service.Rotate(ctx, workspaceID, memberID, created.ID, "", nil, nil); !errors.Is(err, ErrRevoked) {
		t.Fatalf("second rotation of old key error = %v, want ErrRevoked", err)
	}
}

func seedAPIKeyWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) (string, string) {
	t.Helper()
	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ($1,'API key owner',$2,'x',now())`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	created, err := workspaceService.Bootstrap(ctx, userID, "API key workspace", "api-key-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := workspaceService.ActorForUser(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("resolve workspace actor: %v", err)
	}
	return created.ID, actor.MemberID
}
