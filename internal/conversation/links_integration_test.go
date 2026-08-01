//go:build integration

package conversation_test

import (
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestConversationLinksAreWorkspaceScopedSymmetricAndRemovable(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	first, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "First thread")
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	second, _, err := svc.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Follow-up thread")
	if err != nil {
		t.Fatalf("start second: %v", err)
	}

	link, err := svc.Link(ctx, ws.WorkspaceID, ws.MemberID, second.ID, first.ID, "related")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if link.SourceID > link.TargetID || link.Relation != "related" {
		t.Fatalf("link was not canonicalized: %+v", link)
	}
	if _, err := svc.Link(ctx, ws.WorkspaceID, ws.MemberID, first.ID, second.ID, "related"); !errors.Is(err, conversation.ErrLinkAlreadyExists) {
		t.Fatalf("reverse duplicate error = %v, want ErrLinkAlreadyExists", err)
	}

	firstLinks, err := svc.Links(ctx, ws.WorkspaceID, first.ID)
	if err != nil || len(firstLinks) != 1 || firstLinks[0].ID != link.ID {
		t.Fatalf("first links = %+v, err=%v", firstLinks, err)
	}
	secondLinks, err := svc.Links(ctx, ws.WorkspaceID, second.ID)
	if err != nil || len(secondLinks) != 1 || secondLinks[0].ID != link.ID {
		t.Fatalf("second links = %+v, err=%v", secondLinks, err)
	}

	if _, err := svc.Links(ctx, "other-workspace", first.ID); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("cross-workspace links error = %v, want ErrNotFound", err)
	}
	if err := svc.Unlink(ctx, ws.WorkspaceID, ws.MemberID, first.ID, second.ID, "related"); err != nil {
		t.Fatalf("unlink in reverse order: %v", err)
	}
	links, err := svc.Links(ctx, ws.WorkspaceID, first.ID)
	if err != nil || len(links) != 0 {
		t.Fatalf("links after unlink = %+v, err=%v", links, err)
	}
}
