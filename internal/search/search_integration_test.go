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

	if _, _, err := convSvc.Start(ctx, workspaceID, inboxID, "widget", nil, nil, nil, "Visitor", "My refund never arrived"); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Same search term in a different workspace must never surface here.
	if _, _, err := convSvc.Start(ctx, otherWorkspaceID, otherInboxID, "widget", nil, nil, nil, "Visitor", "My refund never arrived either"); err != nil {
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

func TestSearchPageWalksMessagesAndCustomersWithoutRepeats(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	convSvc := conversation.New(pool, events.New(pool), audit.New(pool))
	custSvc := customer.New(pool, events.New(pool), audit.New(pool), config.Limits{MaxEventBytes: 32 << 10, MaxAttributesPerCustomer: 100})
	searchSvc := search.New(convSvc, custSvc)
	workspaceID, inboxID := seedWorkspace(t, ctx, pool)

	for _, body := range []string{"Refund status one", "Refund status two", "Refund status three"} {
		if _, _, err := convSvc.Start(ctx, workspaceID, inboxID, "widget", nil, nil, nil, "Visitor", body); err != nil {
			t.Fatalf("start conversation: %v", err)
		}
	}
	for i, name := range []string{"Refund Customer One", "Refund Customer Two"} {
		id := ids.New(ids.PrefixCustomer)
		if _, err := pool.Exec(ctx, `INSERT INTO customers (id,workspace_id,name,email) VALUES ($1,$2,$3,$4)`, id, workspaceID, name, "refund-"+string(rune('a'+i))+"@example.com"); err != nil {
			t.Fatalf("seed customer: %v", err)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	for pageNumber := 0; pageNumber < 10; pageNumber++ {
		page, err := searchSvc.SearchPage(ctx, workspaceID, "refund", cursor, 2)
		if err != nil {
			t.Fatalf("search page %d: %v", pageNumber, err)
		}
		if len(page.Results) == 0 && page.HasMore {
			t.Fatalf("search page %d claims more rows while empty", pageNumber)
		}
		for _, result := range page.Results {
			if seen[result.Kind+":"+result.EntityID] {
				t.Fatalf("search page %d repeated result %+v", pageNumber, result)
			}
			seen[result.Kind+":"+result.EntityID] = true
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" {
			t.Fatalf("search page %d has_more without cursor", pageNumber)
		}
		cursor = page.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("search returned %d unique results, want 5: %+v", len(seen), seen)
	}
}
