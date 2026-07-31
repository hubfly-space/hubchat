//go:build integration

package feedback

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

type feedbackTestWorkspace struct {
	id       string
	inboxID  string
	memberID string
}

func seedFeedbackWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) feedbackTestWorkspace {
	t.Helper()
	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, 'Feedback Owner', $2, 'x', now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	created, err := workspaceService.Bootstrap(ctx, userID, "Feedback Workspace", "feedback-"+userID[len(userID)-10:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := workspaceService.ActorForUser(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("resolve workspace actor: %v", err)
	}

	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 LIMIT 1`, created.ID).Scan(&inboxID); err != nil {
		t.Fatalf("find default inbox: %v", err)
	}
	return feedbackTestWorkspace{id: created.ID, inboxID: inboxID, memberID: actor.MemberID}
}

func seedFeedbackCustomer(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID string) string {
	t.Helper()
	id := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id,workspace_id,name) VALUES ($1,$2,'Feedback customer')`, id, workspaceID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return id
}

func TestLinksMergeIntoTargetAndRemainWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	workspace := seedFeedbackWorkspace(t, ctx, pool)
	service := New(pool, events.New(pool), audit.New(pool))

	board, err := service.CreateBoard(ctx, workspace.id, BoardInput{Name: "Roadmap", Slug: "roadmap"})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	source, err := service.CreateItem(ctx, workspace.id, board.ID, workspace.memberID, ItemInput{Title: "Source request"}, "")
	if err != nil {
		t.Fatalf("create source item: %v", err)
	}
	target, err := service.CreateItem(ctx, workspace.id, board.ID, workspace.memberID, ItemInput{Title: "Canonical request"}, "")
	if err != nil {
		t.Fatalf("create target item: %v", err)
	}

	firstCustomer := seedFeedbackCustomer(t, ctx, pool, workspace.id)
	secondCustomer := seedFeedbackCustomer(t, ctx, pool, workspace.id)
	if err := service.Vote(ctx, workspace.id, source.ID, firstCustomer); err != nil {
		t.Fatalf("vote for source: %v", err)
	}
	if err := service.Vote(ctx, workspace.id, target.ID, secondCustomer); err != nil {
		t.Fatalf("vote for target: %v", err)
	}
	if err := service.Subscribe(ctx, workspace.id, source.ID, firstCustomer); err != nil {
		t.Fatalf("subscribe to source: %v", err)
	}
	if _, err := service.AddComment(ctx, workspace.id, source.ID, "customer", firstCustomer, "Feedback customer", "Customer note", false); err != nil {
		t.Fatalf("comment on source: %v", err)
	}

	conversationID := ids.New(ids.PrefixConversation)
	if _, err := pool.Exec(ctx, `INSERT INTO conversations (id,workspace_id,inbox_id,channel,subject) VALUES ($1,$2,$3,'manual','Linked conversation')`, conversationID, workspace.id, workspace.inboxID); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	ticketID := ids.New(ids.PrefixTicket)
	if _, err := pool.Exec(ctx, `INSERT INTO tickets (id,workspace_id,number,prefix,title) VALUES ($1,$2,9001,'SUP','Linked ticket')`, ticketID, workspace.id); err != nil {
		t.Fatalf("seed ticket: %v", err)
	}

	conversationLink, err := service.AddLink(ctx, workspace.id, source.ID, workspace.memberID, LinkInput{ConversationID: conversationID})
	if err != nil {
		t.Fatalf("link conversation: %v", err)
	}
	retriedLink, err := service.AddLink(ctx, workspace.id, source.ID, workspace.memberID, LinkInput{ConversationID: conversationID})
	if err != nil {
		t.Fatalf("retry conversation link: %v", err)
	}
	if conversationLink.ID != retriedLink.ID {
		t.Fatalf("retry created a second link: %q then %q", conversationLink.ID, retriedLink.ID)
	}
	if _, err := service.AddLink(ctx, workspace.id, source.ID, workspace.memberID, LinkInput{TicketID: ticketID}); err != nil {
		t.Fatalf("link ticket: %v", err)
	}

	merged, err := service.MergeItems(ctx, workspace.id, source.ID, target.ID, workspace.memberID)
	if err != nil {
		t.Fatalf("merge items: %v", err)
	}
	if merged.ID != target.ID || merged.VoteCount != 2 || merged.CommentCount != 1 || merged.SubscriberCount != 1 {
		t.Fatalf("merged counters = votes %d, comments %d, subscribers %d", merged.VoteCount, merged.CommentCount, merged.SubscriberCount)
	}
	if len(merged.LinkedConversationIDs) != 1 || merged.LinkedConversationIDs[0] != conversationID || len(merged.LinkedTicketIDs) != 1 || merged.LinkedTicketIDs[0] != ticketID {
		t.Fatalf("merged links = conversations %v, tickets %v", merged.LinkedConversationIDs, merged.LinkedTicketIDs)
	}

	var mergedInto *string
	if err := pool.QueryRow(ctx, `SELECT merged_into_id FROM feedback_items WHERE workspace_id=$1 AND id=$2`, workspace.id, source.ID).Scan(&mergedInto); err != nil {
		t.Fatalf("load source redirect: %v", err)
	}
	if mergedInto == nil || *mergedInto != target.ID {
		t.Fatalf("source redirect = %v, want %q", mergedInto, target.ID)
	}
	if _, err := service.MergeItems(ctx, workspace.id, source.ID, target.ID, workspace.memberID); !errors.Is(err, ErrInvalidMerge) {
		t.Fatalf("second merge error = %v, want ErrInvalidMerge", err)
	}

	otherWorkspace := seedFeedbackWorkspace(t, ctx, pool)
	if _, err := service.AddLink(ctx, otherWorkspace.id, source.ID, otherWorkspace.memberID, LinkInput{ConversationID: conversationID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace item link error = %v, want ErrNotFound", err)
	}
}
