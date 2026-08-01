//go:build integration

package file

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

type failingUploadReader struct{}

func (failingUploadReader) Read([]byte) (int, error) {
	return 0, errors.New("simulated upload interruption")
}

func seedFileWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) string {
	t.Helper()
	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, 'File Owner', $2, 'x', now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	service := workspace.New(pool, events.New(pool), audit.New(pool))
	created, err := service.Bootstrap(ctx, userID, "Files", "files-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	return created.ID
}

func TestCreateLeavesRecoverablePendingUploadAndSweepRemovesIt(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	workspaceID := seedFileWorkspace(t, ctx, pool)

	store, err := NewLocalStore(t.TempDir(), 1<<20, []string{"text/plain"})
	if err != nil {
		t.Fatalf("create local store: %v", err)
	}
	service := New(pool, store)
	if _, err := service.Create(ctx, workspaceID, UploadInput{
		Name: "interrupted.txt", MIMEType: "text/plain", SizeBytes: 4,
		Body: failingUploadReader{}, UploadedByType: "system",
	}); err == nil {
		t.Fatal("expected interrupted upload to fail")
	}

	var pending int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM files WHERE workspace_id=$1 AND committed_at IS NULL`, workspaceID).Scan(&pending); err != nil {
		t.Fatalf("count pending uploads: %v", err)
	}
	if pending != 1 {
		t.Fatalf("expected one recoverable pending upload, got %d", pending)
	}

	removed, err := service.SweepAbandoned(ctx, time.Now().UTC().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("sweep abandoned uploads: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected one abandoned upload removed, got %d", removed)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM files WHERE workspace_id=$1`, workspaceID).Scan(&pending); err != nil {
		t.Fatalf("count cleaned uploads: %v", err)
	}
	if pending != 0 {
		t.Fatalf("expected pending metadata to be removed, got %d rows", pending)
	}
}

func TestCommittedUploadIsNotCollectedByAbandonedSweep(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	workspaceID := seedFileWorkspace(t, ctx, pool)

	store, err := NewLocalStore(t.TempDir(), 1<<20, []string{"text/plain"})
	if err != nil {
		t.Fatalf("create local store: %v", err)
	}
	service := New(pool, store)
	record, err := service.Create(ctx, workspaceID, UploadInput{
		Name: "kept.txt", MIMEType: "text/plain", SizeBytes: 5,
		Body: bytes.NewReader([]byte("hello")), UploadedByType: "system",
	})
	if err != nil {
		t.Fatalf("create committed upload: %v", err)
	}

	removed, err := service.SweepAbandoned(ctx, time.Now().UTC().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("sweep committed upload: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected committed upload to remain, removed %d", removed)
	}
	if _, err := service.Get(ctx, workspaceID, record.ID); err != nil {
		t.Fatalf("committed upload was removed: %v", err)
	}
}

func TestAttachToMessageRejectsFilesFromAnotherConversation(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	workspaceID := seedFileWorkspace(t, ctx, pool)

	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 LIMIT 1`, workspaceID).Scan(&inboxID); err != nil {
		t.Fatalf("find inbox: %v", err)
	}
	conversations := conversation.New(pool, events.New(pool), audit.New(pool))
	first, firstMessage, err := conversations.Start(ctx, workspaceID, inboxID, "widget", nil, nil, nil, "Visitor one", "First conversation")
	if err != nil {
		t.Fatalf("start first conversation: %v", err)
	}
	second, _, err := conversations.Start(ctx, workspaceID, inboxID, "widget", nil, nil, nil, "Visitor two", "Second conversation")
	if err != nil {
		t.Fatalf("start second conversation: %v", err)
	}

	store, err := NewLocalStore(t.TempDir(), 1<<20, []string{"text/plain"})
	if err != nil {
		t.Fatalf("create local store: %v", err)
	}
	service := New(pool, store)
	foreign, err := service.Create(ctx, workspaceID, UploadInput{
		Name: "foreign.txt", MIMEType: "text/plain", SizeBytes: 7, Body: bytes.NewReader([]byte("foreign")),
		OwnerType: "conversation", OwnerID: second.ID, UploadedByType: "visitor",
	})
	if err != nil {
		t.Fatalf("create foreign file: %v", err)
	}
	if err := service.AttachToMessage(ctx, workspaceID, firstMessage.ID, []string{foreign.ID}); !errors.Is(err, ErrInvalidAttachment) {
		t.Fatalf("cross-conversation attachment error = %v, want %v", err, ErrInvalidAttachment)
	}

	local, err := service.Create(ctx, workspaceID, UploadInput{
		Name: "local.txt", MIMEType: "text/plain", SizeBytes: 5, Body: bytes.NewReader([]byte("local")),
		OwnerType: "conversation", OwnerID: first.ID, UploadedByType: "visitor",
	})
	if err != nil {
		t.Fatalf("create local file: %v", err)
	}
	if err := service.AttachToMessage(ctx, workspaceID, firstMessage.ID, []string{local.ID}); err != nil {
		t.Fatalf("same-conversation attachment: %v", err)
	}
}
