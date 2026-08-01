//go:build integration

package portal_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/portal"
)

func TestCustomerProfileAndNotificationPreferencesAreScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	workspaceID := ids.New(ids.PrefixWorkspace)
	slug := "pt-" + strings.ToLower(ids.New("s")[2:12])
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug,ticket_prefix) VALUES ($1,'Portal test',$2,'SUP')`, workspaceID, slug); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	customerID := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id,workspace_id,name,email) VALUES ($1,$2,'Before','customer@example.com')`, customerID, workspaceID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	portalID := ids.New(ids.PrefixPortal)
	if _, err := pool.Exec(ctx, `INSERT INTO portals (id,workspace_id,name,subdomain) VALUES ($1,$2,'Help','`+slug+`')`, portalID, workspaceID); err != nil {
		t.Fatalf("seed portal: %v", err)
	}

	svc := portal.New(pool, portal.Options{})
	defaults, err := svc.Preferences(ctx, workspaceID, customerID)
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if !defaults.TicketStatus || !defaults.FeedbackUpdates || defaults.Changelog || !defaults.Surveys {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	session := &portal.Session{
		WorkspaceID: workspaceID,
		CustomerID:  customerID,
		Portal:      &portal.Portal{ID: portalID, WorkspaceID: workspaceID},
	}
	newName := "After"
	falseValue := false
	updated, err := svc.UpdateProfile(ctx, session, portal.ProfileInput{
		Name: &newName,
		Preferences: &portal.NotificationPreferencesInput{
			FeedbackUpdates: &falseValue,
			Changelog:       &falseValue,
		},
	})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("expected name %q, got %q", newName, updated.Name)
	}
	changed, err := svc.Preferences(ctx, workspaceID, customerID)
	if err != nil {
		t.Fatalf("reload preferences: %v", err)
	}
	if changed.FeedbackUpdates || changed.Changelog || !changed.TicketStatus || !changed.Surveys {
		t.Fatalf("partial preference update overwrote defaults: %+v", changed)
	}
	if _, err := svc.Preferences(ctx, ids.New(ids.PrefixWorkspace), customerID); !errors.Is(err, portal.ErrCustomerNotFound) {
		t.Fatalf("expected wrong-workspace lookup to be hidden, got %v", err)
	}
}

func TestUpdateReplacesNavigationWithinWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name, slug) VALUES ('wrk_portal_update', 'Portal Update', 'portal-update')`); err != nil {
		t.Fatal(err)
	}

	service := portal.New(pool, portal.Options{})
	created, err := service.Create(ctx, "wrk_portal_update", portal.CreateRequest{Name: "Customer help", Subdomain: "help"})
	if err != nil {
		t.Fatalf("create portal: %v", err)
	}
	navigation := []portal.NavigationItem{
		{Label: "Requests", Href: "/tickets"},
		{Label: "Status", Href: "https://status.example.com", External: true},
	}
	updated, err := service.Update(ctx, "wrk_portal_update", created.ID, portal.UpdateRequest{Navigation: &navigation})
	if err != nil {
		t.Fatalf("update portal: %v", err)
	}
	if len(updated.Navigation) != len(navigation) {
		t.Fatalf("navigation length = %d, want %d", len(updated.Navigation), len(navigation))
	}
	for i, item := range navigation {
		if updated.Navigation[i].Label != item.Label || updated.Navigation[i].Href != item.Href || updated.Navigation[i].External != item.External {
			t.Fatalf("navigation[%d] = %+v, want %+v", i, updated.Navigation[i], item)
		}
	}

	if _, err := service.Update(ctx, "other-workspace", created.ID, portal.UpdateRequest{Navigation: &navigation}); !errors.Is(err, portal.ErrNotFound) {
		t.Fatalf("cross-workspace update error = %v, want ErrNotFound", err)
	}
}

func TestListPageUsesStableNameCursorAndWorkspaceScope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_portal_page','Portal Page','portal-page'),
			('wrk_portal_other','Portal Other','portal-other');
		INSERT INTO portals (id,workspace_id,name,subdomain) VALUES
			('prl_page_a','wrk_portal_page','Alpha','alpha-page'),
			('prl_page_b','wrk_portal_page','Beta','beta-page'),
			('prl_page_other','wrk_portal_other','Other','other-page')
	`); err != nil {
		t.Fatal(err)
	}
	service := portal.New(pool, portal.Options{})
	first, err := service.ListPage(ctx, "wrk_portal_page", "", "", 1)
	if err != nil || len(first) != 1 || first[0].ID != "prl_page_a" {
		t.Fatalf("first portal page = %#v, err=%v", first, err)
	}
	second, err := service.ListPage(ctx, "wrk_portal_page", first[0].Name, first[0].ID, 1)
	if err != nil || len(second) != 1 || second[0].ID != "prl_page_b" {
		t.Fatalf("second portal page = %#v, err=%v", second, err)
	}
}

func TestCustomDomainLifecycleIsWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name, slug) VALUES ('wrk_domain_a', 'Domain A', 'domain-a'), ('wrk_domain_b', 'Domain B', 'domain-b')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO portals (id,workspace_id,name,subdomain) VALUES ('prl_domain_a','wrk_domain_a','A','domain-a'), ('prl_domain_b','wrk_domain_b','B','domain-b')`); err != nil {
		t.Fatal(err)
	}

	service := portal.New(pool, portal.Options{})
	domain, err := service.AddDomain(ctx, "wrk_domain_a", "prl_domain_a", "Support.Example.com.")
	if err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if domain.Domain != "support.example.com" || domain.Status != "pending" || domain.VerificationToken == "" {
		t.Fatalf("unexpected domain: %+v", domain)
	}
	if _, err := service.Get(ctx, "wrk_domain_b", "prl_domain_a"); !errors.Is(err, portal.ErrNotFound) {
		t.Fatalf("cross-workspace portal lookup = %v, want ErrNotFound", err)
	}
	if err := service.DeleteDomain(ctx, "wrk_domain_b", "prl_domain_a", domain.ID); !errors.Is(err, portal.ErrNotFound) {
		t.Fatalf("cross-workspace domain delete = %v, want ErrNotFound", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_domains SET status='verified' WHERE id=$1`, domain.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err := service.Resolve(ctx, "support.example.com")
	if err != nil {
		t.Fatalf("resolve verified domain: %v", err)
	}
	if resolved.ID != "prl_domain_a" {
		t.Fatalf("resolved portal = %q, want prl_domain_a", resolved.ID)
	}
}

func TestListDomainsPageUsesDomainCursorAndWorkspaceScope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id, name, slug) VALUES ('wrk_domain_page', 'Domain Page', 'domain-page'), ('wrk_domain_other', 'Domain Other', 'domain-other')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO portals (id, workspace_id, name, subdomain) VALUES ('prl_domain_page', 'wrk_domain_page', 'Portal', 'domain-page'), ('prl_domain_other', 'wrk_domain_other', 'Other', 'domain-other')`); err != nil {
		t.Fatal(err)
	}
	service := portal.New(pool, portal.Options{})
	for _, domain := range []string{"z.example.com", "a.example.com"} {
		if _, err := service.AddDomain(ctx, "wrk_domain_page", "prl_domain_page", domain); err != nil {
			t.Fatalf("add domain %s: %v", domain, err)
		}
	}
	if _, err := service.AddDomain(ctx, "wrk_domain_other", "prl_domain_other", "other.example.com"); err != nil {
		t.Fatal(err)
	}
	first, err := service.ListDomainsPage(ctx, "wrk_domain_page", "prl_domain_page", "", "", 2)
	if err != nil || len(first) != 2 || first[0].Domain != "a.example.com" || first[1].Domain != "z.example.com" {
		t.Fatalf("first domain page = %#v, err=%v", first, err)
	}
	second, err := service.ListDomainsPage(ctx, "wrk_domain_page", "prl_domain_page", first[0].Domain, first[0].ID, 2)
	if err != nil || len(second) != 1 || second[0].Domain != "z.example.com" {
		t.Fatalf("second domain page = %#v, err=%v", second, err)
	}
	other, err := service.ListDomainsPage(ctx, "wrk_domain_other", "prl_domain_page", "", "", 2)
	if err != nil || len(other) != 0 {
		t.Fatalf("cross-workspace domain page = %#v, err=%v", other, err)
	}
}

func TestTicketsUsesStableCursorCompanyScopeAndWorkspaceIsolation(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id, name, slug) VALUES
			('wrk_ticket_page', 'Ticket Page', 'ticket-page'),
			('wrk_ticket_other', 'Ticket Other', 'ticket-other');
		INSERT INTO companies (id, workspace_id, name) VALUES ('com_ticket_page', 'wrk_ticket_page', 'Acme');
		INSERT INTO customers (id, workspace_id, name, email) VALUES
			('cus_ticket_owner', 'wrk_ticket_page', 'Owner', 'owner@example.com'),
			('cus_ticket_colleague', 'wrk_ticket_page', 'Colleague', 'colleague@example.com'),
			('cus_ticket_other', 'wrk_ticket_other', 'Other', 'other@example.com');
		INSERT INTO company_customers (company_id, customer_id) VALUES
			('com_ticket_page', 'cus_ticket_owner'),
			('com_ticket_page', 'cus_ticket_colleague');
		INSERT INTO tickets (id, workspace_id, number, prefix, title, customer_id, updated_at) VALUES
			('tkt_ticket_recent', 'wrk_ticket_page', 3, 'SUP', 'Recent colleague', 'cus_ticket_colleague', '2026-01-03T00:00:00Z'),
			('tkt_ticket_owned', 'wrk_ticket_page', 2, 'SUP', 'Owned ticket', 'cus_ticket_owner', '2026-01-02T00:00:00Z'),
			('tkt_ticket_old', 'wrk_ticket_page', 1, 'SUP', 'Old colleague', 'cus_ticket_colleague', '2026-01-01T00:00:00Z'),
			('tkt_ticket_other', 'wrk_ticket_other', 1, 'OTH', 'Other workspace', 'cus_ticket_other', '2026-01-04T00:00:00Z')
	`); err != nil {
		t.Fatalf("seed tickets: %v", err)
	}

	service := portal.New(pool, portal.Options{})
	session := &portal.Session{
		WorkspaceID: "wrk_ticket_page",
		CustomerID:  "cus_ticket_owner",
		Portal:      &portal.Portal{ID: "prl_ticket_page", WorkspaceID: "wrk_ticket_page", Permissions: map[string]any{"view_company_tickets": true}},
	}
	first, err := service.Tickets(ctx, session, portal.TicketFilter{Limit: 2})
	if err != nil || len(first) != 2 || first[0].ID != "tkt_ticket_recent" || first[1].ID != "tkt_ticket_owned" {
		t.Fatalf("first ticket page = %#v, err=%v", first, err)
	}
	second, err := service.Tickets(ctx, session, portal.TicketFilter{Before: first[1].UpdatedAt, BeforeID: first[1].ID, Limit: 2})
	if err != nil || len(second) != 1 || second[0].ID != "tkt_ticket_old" {
		t.Fatalf("second ticket page = %#v, err=%v", second, err)
	}

	session.Portal.Permissions = map[string]any{}
	owned, err := service.Tickets(ctx, session, portal.TicketFilter{Limit: 10})
	if err != nil || len(owned) != 1 || owned[0].ID != "tkt_ticket_owned" {
		t.Fatalf("permission-scoped tickets = %#v, err=%v", owned, err)
	}
}
