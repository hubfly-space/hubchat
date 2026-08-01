//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/knowledgebase"
)

func TestPublicChangelogUsesWorkspaceScopedCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_change_a','Changes A','changes-a'),
			('wrk_change_b','Changes B','changes-b');
		INSERT INTO changelog_entries (id,workspace_id,title,body,kind,published_at) VALUES
			('change_a1','wrk_change_a','Newest','A newest','added','2026-07-31T12:00:00Z'),
			('change_a2','wrk_change_a','Older','A older','fixed','2026-07-30T12:00:00Z'),
			('change_b1','wrk_change_b','Other','B other','improved','2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	request := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		req.SetPathValue("workspaceID", "wrk_change_a")
		response := httptest.NewRecorder()
		handlePublicChangelog(Deps{Knowledgebase: knowledgebase.New(pool)})(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/public/changelog/wrk_change_a?limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0]["id"] != "change_a1" {
		t.Fatalf("first public changelog page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/public/changelog/wrk_change_a?limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0]["id"] != "change_a2" {
		t.Fatalf("second public changelog page = %d %+v", secondResponse.Code, second)
	}
}
