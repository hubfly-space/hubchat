//go:build integration

package form

import (
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestListPublicPageUsesNameCursorAndVisibility(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_public_forms','Public Forms','public-forms');
		INSERT INTO forms (id,workspace_id,name,slug,access,enabled) VALUES
			('frm_public_a','wrk_public_forms','Alpha','alpha','public',true),
			('frm_public_private','wrk_public_forms','Hidden','hidden','authenticated',true),
			('frm_public_b','wrk_public_forms','Beta','beta','public',true)
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	first, err := service.ListPublicPage(ctx, "wrk_public_forms", "", "", 1)
	if err != nil || len(first) != 1 || first[0].Name != "Alpha" {
		t.Fatalf("first public form page = %+v, err=%v", first, err)
	}
	second, err := service.ListPublicPage(ctx, "wrk_public_forms", first[0].Name, first[0].ID, 1)
	if err != nil || len(second) != 1 || second[0].Name != "Beta" {
		t.Fatalf("second public form page = %+v, err=%v", second, err)
	}
}

func TestPortalFormVisibilitySeparatesPublicAndAuthenticatedForms(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_portal_forms','Portal Forms','portal-forms');
		INSERT INTO forms (id,workspace_id,name,slug,access,enabled) VALUES
			('frm_portal_public','wrk_portal_forms','Public','public','public',true),
			('frm_portal_auth','wrk_portal_forms','Private','private','authenticated',true)
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	public, err := service.ListPortalPage(ctx, "wrk_portal_forms", false, time.Time{}, "", 10)
	if err != nil || len(public) != 1 || public[0].Slug != "public" {
		t.Fatalf("anonymous portal forms = %+v, err=%v", public, err)
	}
	if _, err := service.GetPublic(ctx, "wrk_portal_forms", "private"); !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("public authenticated form error = %v, want ErrAuthenticationRequired", err)
	}
	authenticated, err := service.ListPortalPage(ctx, "wrk_portal_forms", true, time.Time{}, "", 10)
	if err != nil || len(authenticated) != 2 {
		t.Fatalf("authenticated portal forms = %+v, err=%v", authenticated, err)
	}
}
