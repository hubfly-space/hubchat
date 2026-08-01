//go:build integration

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestRoleCatalogUsesBoundedWorkspaceCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_role_page_a','Roles A','roles-page-a'),
			('wrk_role_page_b','Roles B','roles-page-b');
		INSERT INTO roles (id,workspace_id,key,name,is_builtin) VALUES
			('rol_role_page_a1','wrk_role_page_a','z_custom_a','Z Custom A',false),
			('rol_role_page_a2','wrk_role_page_a','y_custom_a','Y Custom A',false),
			('rol_role_page_b1','wrk_role_page_b','a_custom_b','A Other Workspace',false)
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Workspace: workspace.New(pool, nil, nil)}
	actor := &authorization.Actor{WorkspaceID: "wrk_role_page_a", Role: "owner"}
	request := func(path string) (Page[roleJSON], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListRoles(deps)(response, req)
		var page Page[roleJSON]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	seen := map[string]bool{}
	cursor := ""
	for pageNumber := 0; pageNumber < 20; pageNumber++ {
		path := "/api/v1/roles?limit=2"
		if cursor != "" {
			path = fmt.Sprintf("%s&cursor=%s", path, cursor)
		}
		page, response := request(path)
		if response.Code != http.StatusOK {
			t.Fatalf("role page %d status=%d body=%s", pageNumber, response.Code, response.Body.String())
		}
		for _, role := range page.Data {
			if role.WorkspaceID != "" && role.WorkspaceID != "wrk_role_page_a" {
				t.Fatalf("role catalog crossed workspace boundary: %+v", role)
			}
			if seen[role.ID] {
				t.Fatalf("role catalog repeated %s on page %d", role.ID, pageNumber)
			}
			seen[role.ID] = true
		}
		if !page.HasMore {
			if page.NextCursor != nil {
				t.Fatal("terminal role page returned a next cursor")
			}
			break
		}
		if page.NextCursor == nil {
			t.Fatal("role page reported more rows without a cursor")
		}
		cursor = *page.NextCursor
	}
	if !seen["rol_role_page_a1"] || !seen["rol_role_page_a2"] {
		t.Fatalf("workspace custom roles were not reachable through pagination: seen=%v", seen)
	}
	if seen["rol_role_page_b1"] {
		t.Fatal("other workspace role was returned")
	}
}
