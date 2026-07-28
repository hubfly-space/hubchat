//go:build integration

package customer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestCompanyCRUDTagsAndRoster(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, memberID := seedWorkspace(t, ctx, pool)

	domain := "acme.example"
	company, err := svc.CreateCompany(ctx, wsID, memberID, "Acme Corp", &domain, nil, nil)
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	if company.CustomerCount != 0 || company.OpenTicketCount != 0 {
		t.Fatalf("expected a fresh company to have zero counts, got %+v", company)
	}

	if _, err := svc.CreateCompany(ctx, wsID, memberID, "  ", nil, nil, nil); !errors.Is(err, customer.ErrInvalidCompanyName) {
		t.Fatalf("expected ErrInvalidCompanyName for blank name, got %v", err)
	}

	tier := "enterprise"
	updated, err := svc.UpdateCompany(ctx, wsID, memberID, company.ID, "Acme Corporation", &domain, nil, &tier, nil)
	if err != nil {
		t.Fatalf("update company: %v", err)
	}
	if updated.Name != "Acme Corporation" || updated.Tier == nil || *updated.Tier != "enterprise" {
		t.Fatalf("expected updated fields to persist, got %+v", updated)
	}

	tagID := seedTag(t, ctx, pool, wsID, "vip")
	if err := svc.AddCompanyTag(ctx, wsID, company.ID, tagID); err != nil {
		t.Fatalf("add company tag: %v", err)
	}
	tags, err := svc.CompanyTags(ctx, wsID, company.ID)
	if err != nil || len(tags) != 1 || tags[0] != tagID {
		t.Fatalf("expected exactly [vip], got %v, %v", tags, err)
	}

	cust := seedCustomer(t, ctx, pool, wsID, "Ada")
	if err := svc.LinkCustomer(ctx, wsID, company.ID, cust); err != nil {
		t.Fatalf("link customer: %v", err)
	}
	reloaded, err := svc.Company(ctx, wsID, company.ID)
	if err != nil {
		t.Fatalf("reload company: %v", err)
	}
	if reloaded.CustomerCount != 1 {
		t.Fatalf("expected customer_count 1 after linking, got %d", reloaded.CustomerCount)
	}
	roster, err := svc.CompanyCustomers(ctx, wsID, company.ID, 10)
	if err != nil || len(roster) != 1 || roster[0].ID != cust {
		t.Fatalf("expected the linked customer in the roster, got %+v, %v", roster, err)
	}

	if err := svc.UnlinkCustomer(ctx, wsID, company.ID, cust); err != nil {
		t.Fatalf("unlink customer: %v", err)
	}
	reloaded, err = svc.Company(ctx, wsID, company.ID)
	if err != nil || reloaded.CustomerCount != 0 {
		t.Fatalf("expected customer_count 0 after unlinking, got %+v, %v", reloaded, err)
	}
}

func TestCompanyExternalIDMustBeUniquePerWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, memberID := seedWorkspace(t, ctx, pool)

	extID := "acct_001"
	if _, err := svc.CreateCompany(ctx, wsID, memberID, "First", nil, &extID, nil); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := svc.CreateCompany(ctx, wsID, memberID, "Second", nil, &extID, nil); !errors.Is(err, customer.ErrCompanyExternalID) {
		t.Fatalf("expected ErrCompanyExternalID for a duplicate external id, got %v", err)
	}
}

func createDef(t *testing.T, ctx context.Context, svc *customer.Service, wsID, key, attrType string, in customer.AttributeDefinitionInput) *customer.AttributeDefinition {
	t.Helper()
	def, err := svc.CreateAttributeDefinition(ctx, wsID, "customer", key, attrType, in)
	if err != nil {
		t.Fatalf("create attribute definition %q: %v", key, err)
	}
	return def
}

func TestSetCustomerAttributesEnforcesTheMetadataSchema(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, memberID := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsID, "Ada")

	// Undeclared key: rejected before anything else.
	if _, err := svc.SetCustomerAttributes(ctx, wsID, memberID, cust, "rest_api", map[string]any{"plan": "enterprise"}); !errors.Is(err, customer.ErrAttrNotDeclared) {
		t.Fatalf("expected ErrAttrNotDeclared, got %v", err)
	}

	createDef(t, ctx, svc, wsID, "plan", "enum", customer.AttributeDefinitionInput{
		Label: "Plan", Options: []string{"starter", "growth", "enterprise"}, AllowedSources: []string{"rest_api"},
	})

	// Declared but wrong value: rejected.
	if _, err := svc.SetCustomerAttributes(ctx, wsID, memberID, cust, "rest_api", map[string]any{"plan": "platinum"}); !errors.Is(err, customer.ErrAttrInvalidValue) {
		t.Fatalf("expected ErrAttrInvalidValue for an option outside the enum, got %v", err)
	}

	// Declared but wrong source: rejected.
	if _, err := svc.SetCustomerAttributes(ctx, wsID, memberID, cust, "js_sdk", map[string]any{"plan": "growth"}); !errors.Is(err, customer.ErrAttrSourceNotAllowed) {
		t.Fatalf("expected ErrAttrSourceNotAllowed for js_sdk (only rest_api is allowed), got %v", err)
	}

	// Valid write.
	updated, err := svc.SetCustomerAttributes(ctx, wsID, memberID, cust, "rest_api", map[string]any{"plan": "growth"})
	if err != nil {
		t.Fatalf("valid attribute write: %v", err)
	}
	if updated.Attributes["plan"] != "growth" {
		t.Fatalf("expected plan=growth to persist, got %v", updated.Attributes)
	}
}

func TestSetCustomerAttributesRejectsBlockedKeyPatterns(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, memberID := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsID, "Ada")

	if _, err := pool.Exec(ctx, `
		INSERT INTO attribute_blocklist (id, workspace_id, pattern) VALUES ('blk_test1', $1, '*password*')
	`, wsID); err != nil {
		t.Fatalf("seed blocklist: %v", err)
	}
	// Declaring the field itself doesn't matter — the blocklist is checked first.
	createDef(t, ctx, svc, wsID, "account_password", "string", customer.AttributeDefinitionInput{
		Label: "Password", AllowedSources: []string{"rest_api"},
	})

	if _, err := svc.SetCustomerAttributes(ctx, wsID, memberID, cust, "rest_api", map[string]any{"account_password": "hunter2"}); !errors.Is(err, customer.ErrAttrBlockedKey) {
		t.Fatalf("expected ErrAttrBlockedKey, got %v", err)
	}
}

func TestIngestEventValidatesSourceAndSizeAndPowersTheTimeline(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, _ := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsID, "Ada")

	if _, err := svc.IngestEvent(ctx, wsID, cust, "page.viewed", "not-a-source", nil, nil); !errors.Is(err, customer.ErrInvalidSource) {
		t.Fatalf("expected ErrInvalidSource, got %v", err)
	}
	if _, err := svc.IngestEvent(ctx, wsID, cust, "", "rest_api", nil, nil); !errors.Is(err, customer.ErrEmptyEventType) {
		t.Fatalf("expected ErrEmptyEventType, got %v", err)
	}

	if _, err := svc.IngestEvent(ctx, wsID, cust, "page.viewed", "rest_api", nil, map[string]any{"path": "/pricing"}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if _, err := svc.IngestEvent(ctx, wsID, cust, "checkout.started", "rest_api", nil, map[string]any{"amount": 42.0}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	timeline, err := svc.Timeline(ctx, wsID, cust, time.Time{}, "", 50)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(timeline) != 2 {
		t.Fatalf("expected 2 events on the timeline, got %d", len(timeline))
	}
	if timeline[0].Type != "checkout.started" {
		t.Fatalf("expected the timeline newest-first, got %s first", timeline[0].Type)
	}

	stream, err := svc.ListEvents(ctx, wsID, "page.viewed", time.Time{}, "", 50)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(stream) != 1 || stream[0].Type != "page.viewed" {
		t.Fatalf("expected exactly one page.viewed event, got %+v", stream)
	}
}

func TestIngestEventRejectsAnOversizedPayload(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, _ := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsID, "Ada")

	huge := make(map[string]any, 1)
	blob := make([]byte, 64<<10)
	for i := range blob {
		blob[i] = 'x'
	}
	huge["blob"] = string(blob)

	if _, err := svc.IngestEvent(ctx, wsID, cust, "big.event", "rest_api", nil, huge); !errors.Is(err, customer.ErrEventTooLarge) {
		t.Fatalf("expected ErrEventTooLarge, got %v", err)
	}
}

func TestMergePreviewExecuteAndReverse(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, memberID := seedWorkspace(t, ctx, pool)
	winner := seedCustomer(t, ctx, pool, wsID, "Ada Lovelace")
	loser := seedCustomer(t, ctx, pool, wsID, "Ada L.")

	tagID := seedTag(t, ctx, pool, wsID, "vip")
	if err := svc.AddTag(ctx, wsID, memberID, loser, tagID); err != nil {
		t.Fatalf("tag loser: %v", err)
	}

	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id = $1 LIMIT 1`, wsID).Scan(&inboxID); err != nil {
		t.Fatalf("find inbox: %v", err)
	}
	var convID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO conversations (id, workspace_id, inbox_id, channel, customer_id, state, priority)
		VALUES ('cnv_test_merge1', $1, $2, 'widget', $3, 'open', 'normal') RETURNING id
	`, wsID, inboxID, loser).Scan(&convID); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	preview, err := svc.PreviewMerge(ctx, wsID, winner, loser)
	if err != nil {
		t.Fatalf("preview merge: %v", err)
	}
	if preview.ConversationCount != 1 || preview.TagCount != 1 {
		t.Fatalf("expected 1 conversation and 1 tag in the preview, got %+v", preview)
	}

	if _, err := svc.Merge(ctx, wsID, memberID, winner, winner); !errors.Is(err, customer.ErrMergeSelf) {
		t.Fatalf("expected ErrMergeSelf, got %v", err)
	}

	record, err := svc.Merge(ctx, wsID, memberID, winner, loser)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}

	if _, err := svc.Get(ctx, wsID, loser); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("expected the loser to be gone after merge, got %v", err)
	}
	var convCustomerID string
	if err := pool.QueryRow(ctx, `SELECT customer_id FROM conversations WHERE id = $1`, convID).Scan(&convCustomerID); err != nil {
		t.Fatalf("reload conversation: %v", err)
	}
	if convCustomerID != winner {
		t.Fatalf("expected the conversation to move to the winner, got customer_id=%s", convCustomerID)
	}
	winnerTags, err := svc.Tags(ctx, wsID, winner)
	if err != nil || len(winnerTags) != 1 || winnerTags[0] != tagID {
		t.Fatalf("expected the winner to inherit the loser's tag, got %v, %v", winnerTags, err)
	}

	// Reverse: the loser comes back, and the conversation moves with it.
	if err := svc.ReverseMerge(ctx, wsID, memberID, record.ID); err != nil {
		t.Fatalf("reverse merge: %v", err)
	}
	restored, err := svc.Get(ctx, wsID, loser)
	if err != nil {
		t.Fatalf("expected the loser to be restored, got %v", err)
	}
	if restored.Name == nil || *restored.Name != "Ada L." {
		t.Fatalf("expected the restored customer's name to match the snapshot, got %+v", restored.Name)
	}
	if err := pool.QueryRow(ctx, `SELECT customer_id FROM conversations WHERE id = $1`, convID).Scan(&convCustomerID); err != nil {
		t.Fatalf("reload conversation after reversal: %v", err)
	}
	if convCustomerID != loser {
		t.Fatalf("expected the conversation to move back to the restored loser, got customer_id=%s", convCustomerID)
	}

	if err := svc.ReverseMerge(ctx, wsID, memberID, record.ID); !errors.Is(err, customer.ErrMergeAlreadyReversed) {
		t.Fatalf("expected ErrMergeAlreadyReversed on a second reversal, got %v", err)
	}
}

func TestRetentionSweepDeletesExpiredEventsButKeepsFreshOnes(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, _ := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsID, "Ada")

	if _, err := pool.Exec(ctx, `
		UPDATE workspaces SET settings = '{"privacy": {"retention_days": {"events": 7}}}'::jsonb WHERE id = $1
	`, wsID); err != nil {
		t.Fatalf("set retention policy: %v", err)
	}

	oldID := "cev_test_old01"
	if _, err := pool.Exec(ctx, `
		INSERT INTO customer_events (id, workspace_id, customer_id, type, source, occurred_at)
		VALUES ($1, $2, $3, 'page.viewed', 'rest_api', now() - interval '30 days')
	`, oldID, wsID, cust); err != nil {
		t.Fatalf("seed old event: %v", err)
	}
	if _, err := svc.IngestEvent(ctx, wsID, cust, "page.viewed", "rest_api", nil, nil); err != nil {
		t.Fatalf("ingest fresh event: %v", err)
	}

	eventsDeleted, _, err := svc.RunRetentionSweep(ctx)
	if err != nil {
		t.Fatalf("retention sweep: %v", err)
	}
	if eventsDeleted != 1 {
		t.Fatalf("expected exactly 1 expired event deleted, got %d", eventsDeleted)
	}

	remaining, err := svc.ListEvents(ctx, wsID, "", time.Time{}, "", 50)
	if err != nil {
		t.Fatalf("list remaining events: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID == oldID {
		t.Fatalf("expected only the fresh event to remain, got %+v", remaining)
	}
}

func TestExportBundleAndDeleteAnonymises(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	svc := newTestService(t, pool)
	wsID, memberID := seedWorkspace(t, ctx, pool)
	cust := seedCustomer(t, ctx, pool, wsID, "Ada")

	if _, err := pool.Exec(ctx, `UPDATE customers SET email = 'ada@example.com' WHERE id = $1`, cust); err != nil {
		t.Fatalf("seed email: %v", err)
	}
	if _, err := svc.IngestEvent(ctx, wsID, cust, "page.viewed", "rest_api", nil, nil); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	bundle, err := svc.Export(ctx, wsID, cust)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if bundle.Customer.Email == nil || *bundle.Customer.Email != "ada@example.com" {
		t.Fatalf("expected the export to include the customer's email, got %+v", bundle.Customer.Email)
	}
	if len(bundle.Events) != 1 {
		t.Fatalf("expected 1 event in the export bundle, got %d", len(bundle.Events))
	}

	if err := svc.Delete(ctx, wsID, memberID, cust); err != nil {
		t.Fatalf("delete: %v", err)
	}
	anonymised, err := svc.Get(ctx, wsID, cust)
	if err != nil {
		t.Fatalf("expected the row to still exist after anonymising, got %v", err)
	}
	if anonymised.Email != nil || anonymised.Name != nil {
		t.Fatalf("expected name/email cleared, got %+v", anonymised)
	}
	remaining, err := svc.Timeline(ctx, wsID, cust, time.Time{}, "", 50)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("expected the event history erased, got %v, %v", remaining, err)
	}
}
