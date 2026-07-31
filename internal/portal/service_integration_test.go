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
