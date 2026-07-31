//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/savedview"
)

func TestSavedViewListUsesCursorAndVisibilityScope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_views_a','Views A','views-a'), ('wrk_views_b','Views B','views-b');
		INSERT INTO users (id,name,email) VALUES
			('usr_views_a','Views A','views-a@example.com'), ('usr_views_b','Views B','views-b@example.com');
		INSERT INTO workspace_members (id,workspace_id,user_id,role) VALUES
			('mem_views_a','wrk_views_a','usr_views_a','agent'), ('mem_views_b','wrk_views_b','usr_views_b','agent');
		INSERT INTO saved_views (id,workspace_id,name,scope,owner_id,filters,position) VALUES
			('svw_a1','wrk_views_a','Personal A','personal','mem_views_a','{}',1),
			('svw_a2','wrk_views_a','Workspace A','workspace',NULL,'{}',2),
			('svw_b1','wrk_views_b','Other workspace','workspace',NULL,'{}',1)
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{SavedView: savedview.New(pool, nil, nil)}
	actor := &authorization.Actor{WorkspaceID: "wrk_views_a", MemberID: "mem_views_a", Role: "agent"}
	request := func(path string) (Page[savedview.View], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListSavedViews(deps)(response, req)
		var page Page[savedview.View]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/saved-views?entity_type=conversation&limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0].ID != "svw_a1" {
		t.Fatalf("first saved-view page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/saved-views?entity_type=conversation&limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0].ID != "svw_a2" {
		t.Fatalf("second saved-view page = %d %+v", secondResponse.Code, second)
	}
}
