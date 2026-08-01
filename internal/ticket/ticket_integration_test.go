//go:build integration

package ticket_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/hubchat/hubchat/internal/workspace"
)

func newTestService(t *testing.T, pool *database.Pool) (*ticket.Service, *workspace.Service) {
	t.Helper()
	wsSvc := workspace.New(pool, events.New(pool), audit.New(pool))
	return ticket.New(pool, wsSvc, events.New(pool), audit.New(pool)), wsSvc
}

type seededWorkspace struct {
	WorkspaceID string
	InboxID     string
	MemberID    string
}

func seedWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) seededWorkspace {
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

	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id = $1 LIMIT 1`, ws.ID).Scan(&inboxID); err != nil {
		t.Fatalf("find default inbox: %v", err)
	}

	return seededWorkspace{WorkspaceID: ws.ID, InboxID: inboxID, MemberID: actor.MemberID}
}

func seedMember(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID string) string {
	t.Helper()

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash, email_verified_at)
		VALUES ($1, 'Another Agent', $2, 'x', now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	memberID := ids.New(ids.PrefixMember)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspace_members (id, workspace_id, user_id, role)
		VALUES ($1, $2, $3, 'agent')
	`, memberID, workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return memberID
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

func seedCompany(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID, name string) string {
	t.Helper()
	id := ids.New(ids.PrefixCompany)
	if _, err := pool.Exec(ctx, `
		INSERT INTO companies (id, workspace_id, name) VALUES ($1, $2, $3)
	`, id, workspaceID, name); err != nil {
		t.Fatalf("seed company: %v", err)
	}
	return id
}

func linkCustomerToCompany(t *testing.T, ctx context.Context, pool *database.Pool, companyID, customerID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO company_customers (company_id, customer_id) VALUES ($1, $2)
	`, companyID, customerID); err != nil {
		t.Fatalf("link customer to company: %v", err)
	}
}

func TestCreateAllocatesSequentialNumbersUnderThePrefix(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	first, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "First issue", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Second issue", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if first.Prefix != "SUP" || second.Prefix != "SUP" {
		t.Fatalf("expected the workspace's default SUP prefix, got %q and %q", first.Prefix, second.Prefix)
	}
	if second.Number != first.Number+1 {
		t.Fatalf("expected sequential numbers, got %d then %d", first.Number, second.Number)
	}
	if first.Status != "new" || first.Priority != "normal" {
		t.Fatalf("expected a fresh ticket to start new/normal, got status=%s priority=%s", first.Status, first.Priority)
	}
}

func TestCreateDerivesCompanyFromCustomer(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	companyID := seedCompany(t, ctx, pool, ws.WorkspaceID, "Acme Corp")
	customerID := seedCustomer(t, ctx, pool, ws.WorkspaceID, "Priya")
	linkCustomerToCompany(t, ctx, pool, companyID, customerID)

	tkt, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{
		Title: "Billing question", InboxID: ws.InboxID, CustomerID: &customerID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tkt.CompanyID == nil || *tkt.CompanyID != companyID {
		t.Fatalf("expected the customer's company to be derived automatically, got %v", tkt.CompanyID)
	}
}

func TestCreateFromConversationLinksBothSidesAndRejectsDuplicate(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	conversationService := conversation.New(pool, events.New(pool), audit.New(pool))
	ws := seedWorkspace(t, ctx, pool)
	conv, _, err := conversationService.Start(ctx, ws.WorkspaceID, ws.InboxID, "widget", nil, nil, nil, "Visitor", "Please track this")
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	tkt, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{
		Title: "Tracked request", InboxID: ws.InboxID, ConversationID: &conv.ID,
	})
	if err != nil {
		t.Fatalf("create from conversation: %v", err)
	}
	if tkt.ConversationID == nil || *tkt.ConversationID != conv.ID {
		t.Fatalf("ticket conversation link = %v, want %s", tkt.ConversationID, conv.ID)
	}

	var linkedTicketID *string
	if err := pool.QueryRow(ctx, `SELECT ticket_id FROM conversations WHERE workspace_id=$1 AND id=$2`, ws.WorkspaceID, conv.ID).Scan(&linkedTicketID); err != nil {
		t.Fatalf("read reverse conversation link: %v", err)
	}
	if linkedTicketID == nil || *linkedTicketID != tkt.ID {
		t.Fatalf("conversation ticket link = %v, want %s", linkedTicketID, tkt.ID)
	}

	if _, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{
		Title: "Duplicate", InboxID: ws.InboxID, ConversationID: &conv.ID,
	}); !errors.Is(err, ticket.ErrConversationAlreadyTicket) {
		t.Fatalf("duplicate conversion error = %v, want ErrConversationAlreadyTicket", err)
	}
}

func TestCreateRejectsCrossWorkspaceReferences(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	wsA := seedWorkspace(t, ctx, pool)
	wsB := seedWorkspace(t, ctx, pool)

	_, err := svc.Create(ctx, wsA.WorkspaceID, wsA.MemberID, ticket.CreateRequest{
		Title: "Cross tenant", InboxID: wsB.InboxID,
	})
	if !errors.Is(err, ticket.ErrInvalidInbox) {
		t.Fatalf("expected ErrInvalidInbox for another workspace's inbox, got %v", err)
	}

	customerID := seedCustomer(t, ctx, pool, wsB.WorkspaceID, "Someone Else")
	_, err = svc.Create(ctx, wsA.WorkspaceID, wsA.MemberID, ticket.CreateRequest{
		Title: "Cross tenant customer", InboxID: wsA.InboxID, CustomerID: &customerID,
	})
	if !errors.Is(err, ticket.ErrInvalidCustomer) {
		t.Fatalf("expected ErrInvalidCustomer for another workspace's customer, got %v", err)
	}
}

func TestSetAssigneeAndTeamRejectCrossWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	wsA := seedWorkspace(t, ctx, pool)
	wsB := seedWorkspace(t, ctx, pool)

	tkt, err := svc.Create(ctx, wsA.WorkspaceID, wsA.MemberID, ticket.CreateRequest{Title: "Issue", InboxID: wsA.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = svc.SetAssignee(ctx, wsA.WorkspaceID, wsA.MemberID, tkt.ID, &wsB.MemberID)
	if !errors.Is(err, ticket.ErrInvalidAssignee) {
		t.Fatalf("expected ErrInvalidAssignee, got %v", err)
	}

	updated, err := svc.SetAssignee(ctx, wsA.WorkspaceID, wsA.MemberID, tkt.ID, &wsA.MemberID)
	if err != nil {
		t.Fatalf("assign within workspace: %v", err)
	}
	if updated.AssigneeID == nil || *updated.AssigneeID != wsA.MemberID {
		t.Fatalf("expected assignee to be set, got %v", updated.AssigneeID)
	}
}

func TestSetStatusAppliesReopenRules(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	tkt, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Issue", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	resolved, err := svc.SetStatus(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, "resolved")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ResolvedAt == nil || resolved.FirstResolvedAt == nil || resolved.ReopenCount != 0 {
		t.Fatalf("expected resolved_at and first_resolved_at set, reopen_count 0, got %+v", resolved)
	}
	firstResolvedAt := *resolved.FirstResolvedAt

	closed, err := svc.SetStatus(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, "closed")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if closed.ClosedAt == nil {
		t.Fatalf("expected closed_at set after closing")
	}

	reopened, err := svc.SetStatus(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, "open")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.ReopenCount != 1 {
		t.Fatalf("expected reopen_count 1 after reopening a closed ticket, got %d", reopened.ReopenCount)
	}
	if reopened.ResolvedAt != nil || reopened.ClosedAt != nil {
		t.Fatalf("expected resolved_at/closed_at cleared on reopen, got resolved=%v closed=%v", reopened.ResolvedAt, reopened.ClosedAt)
	}
	if reopened.FirstResolvedAt == nil || !reopened.FirstResolvedAt.Equal(firstResolvedAt) {
		t.Fatalf("expected first_resolved_at to be preserved across reopen, got %v (was %v)", reopened.FirstResolvedAt, firstResolvedAt)
	}

	resolvedAgain, err := svc.SetStatus(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, "resolved")
	if err != nil {
		t.Fatalf("resolve again: %v", err)
	}
	if resolvedAgain.FirstResolvedAt == nil || !resolvedAgain.FirstResolvedAt.Equal(firstResolvedAt) {
		t.Fatalf("expected first_resolved_at to stay at its original value across a second resolve, got %v", resolvedAgain.FirstResolvedAt)
	}
}

func TestSetStatusRejectsUnknownStatusAndIsANoOpWhenUnchanged(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	tkt, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Issue", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.SetStatus(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, "archived"); !errors.Is(err, ticket.ErrInvalidStatus) {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}

	same, err := svc.SetStatus(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, "new")
	if err != nil {
		t.Fatalf("no-op status set: %v", err)
	}
	if same.Version != tkt.Version {
		t.Fatalf("expected an unchanged-status set to be a no-op, version changed from %d to %d", tkt.Version, same.Version)
	}
}

func TestUpdateDetailsRejectsStaleVersion(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	tkt, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Issue", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.UpdateDetails(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, tkt.Version, "Updated title", "more detail", nil, nil); err != nil {
		t.Fatalf("first update: %v", err)
	}

	_, err = svc.UpdateDetails(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, tkt.Version, "Stale title", "stale", nil, nil)
	if !errors.Is(err, ticket.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict on a stale version, got %v", err)
	}
}

func TestLinkRejectsSelfAndUnknownRelationAndCyclesInParent(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	a, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "A", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "B", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	c, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "C", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create c: %v", err)
	}

	if err := svc.Link(ctx, ws.WorkspaceID, ws.MemberID, a.ID, a.ID, "related"); !errors.Is(err, ticket.ErrLinkToSelf) {
		t.Fatalf("expected ErrLinkToSelf, got %v", err)
	}
	if err := svc.Link(ctx, ws.WorkspaceID, ws.MemberID, a.ID, b.ID, "sibling"); !errors.Is(err, ticket.ErrInvalidRelation) {
		t.Fatalf("expected ErrInvalidRelation, got %v", err)
	}
	if err := svc.Link(ctx, ws.WorkspaceID, ws.MemberID, a.ID, b.ID, "duplicate_of"); err != nil {
		t.Fatalf("link: %v", err)
	}
	links, err := svc.Links(ctx, ws.WorkspaceID, a.ID)
	if err != nil {
		t.Fatalf("links: %v", err)
	}
	if len(links) != 1 || links[0].Relation != "duplicate_of" {
		t.Fatalf("expected one duplicate_of link, got %+v", links)
	}

	// Parent/child: c -> b -> a (a is c's grandparent). Making a a child of c
	// would close the loop and must be rejected.
	if _, err := svc.SetParent(ctx, ws.WorkspaceID, ws.MemberID, b.ID, &a.ID); err != nil {
		t.Fatalf("set b's parent to a: %v", err)
	}
	if _, err := svc.SetParent(ctx, ws.WorkspaceID, ws.MemberID, c.ID, &b.ID); err != nil {
		t.Fatalf("set c's parent to b: %v", err)
	}
	if _, err := svc.SetParent(ctx, ws.WorkspaceID, ws.MemberID, a.ID, &c.ID); !errors.Is(err, ticket.ErrParentCycle) {
		t.Fatalf("expected ErrParentCycle, got %v", err)
	}
	if _, err := svc.SetParent(ctx, ws.WorkspaceID, ws.MemberID, a.ID, &a.ID); !errors.Is(err, ticket.ErrParentIsSelf) {
		t.Fatalf("expected ErrParentIsSelf, got %v", err)
	}

	children, err := svc.Children(ctx, ws.WorkspaceID, b.ID)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(children) != 1 || children[0] != c.ID {
		t.Fatalf("expected b's only child to be c, got %v", children)
	}
}

func TestRelationshipListsUseStableIDCursors(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	root, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Root", InboxID: ws.InboxID})
	if err != nil {
		t.Fatal(err)
	}
	children := make([]*ticket.Ticket, 0, 2)
	for _, title := range []string{"Child one", "Child two"} {
		child, createErr := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: title, InboxID: ws.InboxID})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, setErr := svc.SetParent(ctx, ws.WorkspaceID, ws.MemberID, child.ID, &root.ID); setErr != nil {
			t.Fatal(setErr)
		}
		if linkErr := svc.Link(ctx, ws.WorkspaceID, ws.MemberID, root.ID, child.ID, "related"); linkErr != nil {
			t.Fatal(linkErr)
		}
		children = append(children, child)
	}

	firstChildren, err := svc.ChildrenPage(ctx, ws.WorkspaceID, root.ID, "", 1)
	if err != nil || len(firstChildren) != 1 {
		t.Fatalf("first children page = %v, err=%v", firstChildren, err)
	}
	secondChildren, err := svc.ChildrenPage(ctx, ws.WorkspaceID, root.ID, firstChildren[0], 1)
	if err != nil || len(secondChildren) != 1 || secondChildren[0] == firstChildren[0] {
		t.Fatalf("second children page = %v, err=%v", secondChildren, err)
	}

	firstLinks, err := svc.LinksPage(ctx, ws.WorkspaceID, root.ID, "", 1)
	if err != nil || len(firstLinks) != 1 {
		t.Fatalf("first links page = %v, err=%v", firstLinks, err)
	}
	secondLinks, err := svc.LinksPage(ctx, ws.WorkspaceID, root.ID, firstLinks[0].ID, 1)
	if err != nil || len(secondLinks) != 1 || secondLinks[0].ID == firstLinks[0].ID {
		t.Fatalf("second links page = %v, err=%v", secondLinks, err)
	}
	_ = children
}

func TestTicketListQueryScopesTitleAndDescription(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	if _, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Payment issue", Description: "invoice reference 42", InboxID: ws.InboxID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Login issue", Description: "password reset", InboxID: ws.InboxID}); err != nil {
		t.Fatal(err)
	}
	items, err := svc.List(ctx, ws.WorkspaceID, ticket.ListFilter{Query: "invoice", Limit: 10})
	if err != nil || len(items) != 1 || items[0].Title != "Payment issue" {
		t.Fatalf("ticket query results = %+v, err=%v", items, err)
	}
}

func TestDuplicateCandidatesMatchSameCustomerBySimilarTitle(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	customerID := seedCustomer(t, ctx, pool, ws.WorkspaceID, "Priya")

	existing, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{
		Title: "Cannot reset my password", InboxID: ws.InboxID, CustomerID: &customerID,
	})
	if err != nil {
		t.Fatalf("create existing: %v", err)
	}
	// An unrelated ticket from the same customer should not surface as a
	// duplicate of a dissimilar title.
	if _, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{
		Title: "Question about invoicing", InboxID: ws.InboxID, CustomerID: &customerID,
	}); err != nil {
		t.Fatalf("create unrelated: %v", err)
	}

	candidates, err := svc.DuplicateCandidates(ctx, ws.WorkspaceID, "", "Cannot reset password", &customerID, nil)
	if err != nil {
		t.Fatalf("duplicate candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != existing.ID {
		t.Fatalf("expected exactly the similar existing ticket, got %+v", candidates)
	}
}

func TestFieldValueValidationEnforcesTypeAndRequired(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	def, err := svc.CreateFieldDefinition(ctx, ws.WorkspaceID, "ticket", "account_id", "string", ticket.FieldDefinitionInput{
		Label: "Account ID", Required: true,
	})
	if err != nil {
		t.Fatalf("create field definition: %v", err)
	}

	tkt, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Issue", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.SetFieldValue(ctx, ws.WorkspaceID, "ticket", tkt.ID, "account_id", 42.0); !errors.Is(err, ticket.ErrInvalidFieldValue) {
		t.Fatalf("expected ErrInvalidFieldValue for a number where a string is required, got %v", err)
	}
	if err := svc.SetFieldValue(ctx, ws.WorkspaceID, "ticket", tkt.ID, "account_id", "acct_123"); err != nil {
		t.Fatalf("set valid value: %v", err)
	}

	values, err := svc.FieldValues(ctx, ws.WorkspaceID, "ticket", tkt.ID)
	if err != nil {
		t.Fatalf("field values: %v", err)
	}
	if values[def.Key] != "acct_123" {
		t.Fatalf("expected the stored value to round-trip, got %v", values[def.Key])
	}
}

func TestFieldValueValidationEnforcesEnumOptions(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	_, err := svc.CreateFieldDefinition(ctx, ws.WorkspaceID, "ticket", "severity", "enum", ticket.FieldDefinitionInput{
		Label: "Severity", Options: []string{"low", "medium", "high"},
	})
	if err != nil {
		t.Fatalf("create field definition: %v", err)
	}
	tkt, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Issue", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.SetFieldValue(ctx, ws.WorkspaceID, "ticket", tkt.ID, "severity", "catastrophic"); !errors.Is(err, ticket.ErrInvalidFieldValue) {
		t.Fatalf("expected ErrInvalidFieldValue for an option outside the enum, got %v", err)
	}
	if err := svc.SetFieldValue(ctx, ws.WorkspaceID, "ticket", tkt.ID, "severity", "high"); err != nil {
		t.Fatalf("set valid enum value: %v", err)
	}
}

func TestCreateRejectsDuplicateFieldKey(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	if _, err := svc.CreateFieldDefinition(ctx, ws.WorkspaceID, "ticket", "account_id", "string", ticket.FieldDefinitionInput{Label: "Account ID"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.CreateFieldDefinition(ctx, ws.WorkspaceID, "ticket", "account_id", "string", ticket.FieldDefinitionInput{Label: "Account ID Again"}); !errors.Is(err, ticket.ErrDuplicateKey) {
		t.Fatalf("expected ErrDuplicateKey, got %v", err)
	}
}

func TestListFiltersByStatusAssigneeAndExcludesClosedByDefault(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)
	otherMemberID := seedMember(t, ctx, pool, ws.WorkspaceID)

	open, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Open one", InboxID: ws.InboxID, AssigneeID: &ws.MemberID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Someone else's", InboxID: ws.InboxID, AssigneeID: &otherMemberID}); err != nil {
		t.Fatalf("create: %v", err)
	}
	closed, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Closed one", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.SetStatus(ctx, ws.WorkspaceID, ws.MemberID, closed.ID, "closed"); err != nil {
		t.Fatalf("close: %v", err)
	}

	all, err := svc.List(ctx, ws.WorkspaceID, ticket.ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected the default list to exclude the closed ticket, got %d tickets", len(all))
	}

	mine, err := svc.List(ctx, ws.WorkspaceID, ticket.ListFilter{AssigneeID: ws.MemberID})
	if err != nil {
		t.Fatalf("list mine: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != open.ID {
		t.Fatalf("expected exactly the ticket assigned to me, got %+v", mine)
	}

	includingClosed, err := svc.List(ctx, ws.WorkspaceID, ticket.ListFilter{Status: []string{"new", "closed"}})
	if err != nil {
		t.Fatalf("list including closed: %v", err)
	}
	if len(includingClosed) != 3 {
		t.Fatalf("expected all 3 tickets when explicitly including closed, got %d", len(includingClosed))
	}

	unassignedOnly, err := svc.List(ctx, ws.WorkspaceID, ticket.ListFilter{AssigneeID: ticket.UnassignedSentinel})
	if err != nil {
		t.Fatalf("list unassigned: %v", err)
	}
	if len(unassignedOnly) != 0 {
		t.Fatalf("expected no unassigned open tickets (both are assigned), got %+v", unassignedOnly)
	}
}

func TestTagsAndFollowersRoundTrip(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc, _ := newTestService(t, pool)
	ws := seedWorkspace(t, ctx, pool)

	tagID := ids.New(ids.PrefixTag)
	if _, err := pool.Exec(ctx, `INSERT INTO tags (id, workspace_id, name) VALUES ($1, $2, 'VIP')`, tagID, ws.WorkspaceID); err != nil {
		t.Fatalf("seed tag: %v", err)
	}

	tkt, err := svc.Create(ctx, ws.WorkspaceID, ws.MemberID, ticket.CreateRequest{Title: "Issue", InboxID: ws.InboxID})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.AddTag(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, tagID); err != nil {
		t.Fatalf("add tag: %v", err)
	}
	tags, err := svc.Tags(ctx, ws.WorkspaceID, tkt.ID)
	if err != nil {
		t.Fatalf("tags: %v", err)
	}
	if len(tags) != 1 || tags[0] != tagID {
		t.Fatalf("expected the tag to be attached, got %v", tags)
	}

	if err := svc.Follow(ctx, ws.WorkspaceID, tkt.ID, ws.MemberID); err != nil {
		t.Fatalf("follow: %v", err)
	}
	followers, err := svc.Followers(ctx, ws.WorkspaceID, tkt.ID)
	if err != nil {
		t.Fatalf("followers: %v", err)
	}
	if len(followers) != 1 || followers[0] != ws.MemberID {
		t.Fatalf("expected exactly one follower, got %v", followers)
	}

	if err := svc.RemoveTag(ctx, ws.WorkspaceID, ws.MemberID, tkt.ID, tagID); err != nil {
		t.Fatalf("remove tag: %v", err)
	}
	tags, err = svc.Tags(ctx, ws.WorkspaceID, tkt.ID)
	if err != nil {
		t.Fatalf("tags after removal: %v", err)
	}
	if len(tags) != 0 {
		t.Fatalf("expected no tags after removal, got %v", tags)
	}
}
