//go:build integration

package form

import (
	"testing"

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
