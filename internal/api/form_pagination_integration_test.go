//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/form"
)

func TestFormListUsesWorkspaceCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_forms_a','Forms A','forms-a'),
			('wrk_forms_b','Forms B','forms-b');
		INSERT INTO forms (id,workspace_id,name,slug,created_at,updated_at) VALUES
			('frm_a1','wrk_forms_a','Newest A','newest-a','2026-07-31T12:00:00Z','2026-07-31T12:00:00Z'),
			('frm_a2','wrk_forms_a','Older A','older-a','2026-07-30T12:00:00Z','2026-07-30T12:00:00Z'),
			('frm_b1','wrk_forms_b','Other B','other-b','2026-07-31T13:00:00Z','2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Form: form.New(pool)}
	actor := &authorization.Actor{WorkspaceID: "wrk_forms_a", Role: "owner"}
	request := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListForms(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/forms?limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0]["id"] != "frm_a1" {
		t.Fatalf("first form page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/forms?limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0]["id"] != "frm_a2" {
		t.Fatalf("second form page = %d %+v", secondResponse.Code, second)
	}
}
