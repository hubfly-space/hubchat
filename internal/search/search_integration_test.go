//go:build integration

package search_test

import (
	"context"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/search"
	"github.com/hubchat/hubchat/internal/workspace"
)

func seedWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) (workspaceID, inboxID string) {
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

	var inbox string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id = $1 LIMIT 1`, ws.ID).Scan(&inbox); err != nil {
		t.Fatalf("find default inbox: %v", err)
	}
	return ws.ID, inbox
}

// A global search reaches both message content and customer directory
// entries, each carrying enough to render and link to without another
// lookup — the two entity kinds this stage actually has to search.
func TestSearchFindsBothMessagesAndCustomers(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	convSvc := conversation.New(pool, events.New(pool), audit.New(pool))
	custSvc := customer.New(pool, events.New(pool), audit.New(pool), config.Limits{MaxEventBytes: 32 << 10, MaxAttributesPerCustomer: 100})
	searchSvc := search.New(convSvc, custSvc)

	workspaceID, inboxID := seedWorkspace(t, ctx, pool)
	otherWorkspaceID, otherInboxID := seedWorkspace(t, ctx, pool)

	if _, _, err := convSvc.Start(ctx, workspaceID, inboxID, "widget", nil, nil, "Visitor", "My refund never arrived"); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Same search term in a different workspace must never surface here.
	if _, _, err := convSvc.Start(ctx, otherWorkspaceID, otherInboxID, "widget", nil, nil, "Visitor", "My refund never arrived either"); err != nil {
		t.Fatalf("start in other workspace: %v", err)
	}
	custID := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `
		INSERT INTO customers (id, workspace_id, name, email) VALUES ($1, $2, 'Refund Customer', 'refund@example.com')
	`, custID, workspaceID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	results, err := searchSvc.Search(ctx, workspaceID, "refund", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	var sawMessage, sawCustomer bool
	var messageHits int
	for _, r := range results {
		switch r.Kind {
		case "message":
			sawMessage = true
			messageHits++
			if r.ConversationID == "" {
				t.Fatalf("message result missing conversation id: %+v", r)
			}
		case "customer":
			sawCustomer = true
			if r.EntityID != custID {
				t.Fatalf("expected customer result for %s, got %+v", custID, r)
			}
		}
	}
	if messageHits != 1 {
		t.Fatalf("expected exactly one message hit (this workspace's), got %d — the other workspace's match leaked", messageHits)
	}
	if !sawMessage {
		t.Fatalf("expected a message hit for 'refund', got %+v", results)
	}
	if !sawCustomer {
		t.Fatalf("expected a customer hit for 'refund', got %+v", results)
	}
}

// A query with no match anywhere returns an empty list, not an error — the
// CommandPalette's normal "nothing found yet" state.
func TestSearchReturnsEmptyWithoutError(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	convSvc := conversation.New(pool, events.New(pool), audit.New(pool))
	custSvc := customer.New(pool, events.New(pool), audit.New(pool), config.Limits{MaxEventBytes: 32 << 10, MaxAttributesPerCustomer: 100})
	searchSvc := search.New(convSvc, custSvc)

	workspaceID, _ := seedWorkspace(t, ctx, pool)

	results, err := searchSvc.Search(ctx, workspaceID, "nonexistentxyz123", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results, got %+v", results)
	}
}
