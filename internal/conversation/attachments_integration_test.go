//go:build integration

package conversation_test

import (
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/file"
)

func TestRejectedAttachmentsRollBackMessagesAndEvents(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	ws := seedWorkspace(t, ctx, pool)
	svc := newTestService(t, pool)
	store, err := file.NewLocalStore(t.TempDir(), 1<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	attachments := conversation.Attachments{IDs: []string{"fil_does_not_exist"}, Store: file.New(pool, store)}
	count := func(table string) int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE workspace_id=$1", ws.WorkspaceID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count("workspace_events")
	if _, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "manual", nil, nil, nil, "Agent", "Bad attachment", attachments); !errors.Is(err, file.ErrInvalidAttachment) {
		t.Fatalf("start error=%v", err)
	}
	if count("conversations") != 0 || count("messages") != 0 || count("workspace_events") != before {
		t.Fatal("rejected start left conversation/message/events")
	}
	conv, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "manual", nil, nil, nil, "Agent", "Opening")
	if err != nil {
		t.Fatal(err)
	}
	before = count("workspace_events")
	clientID := "retryable-send"
	if _, err := svc.PostMessage(ctx, ws.WorkspaceID, conv.ID, &clientID, "reply", "agent", &ws.MemberID, "Agent", "Reply", attachments); !errors.Is(err, file.ErrInvalidAttachment) {
		t.Fatalf("reply error=%v", err)
	}
	if count("messages") != 1 || count("workspace_events") != before {
		t.Fatal("rejected reply left message/events")
	}
	if _, err := svc.PostMessage(ctx, ws.WorkspaceID, conv.ID, &clientID, "reply", "agent", &ws.MemberID, "Agent", "Reply"); err != nil {
		t.Fatal(err)
	}
	if count("messages") != 2 {
		t.Fatal("retry after failure did not save exactly one reply")
	}
}
