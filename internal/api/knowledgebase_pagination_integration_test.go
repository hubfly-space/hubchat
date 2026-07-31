//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/knowledgebase"
)

func TestArticleListUsesDeterministicWorkspaceCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_kb_a','KB A','kb-a'),
			('wrk_kb_b','KB B','kb-b');
		INSERT INTO knowledge_bases (id,workspace_id,name,slug) VALUES
			('kb_a','wrk_kb_a','A Help','a-help'),
			('kb_b','wrk_kb_b','B Help','b-help');
		INSERT INTO articles (id,workspace_id,knowledge_base_id,title,slug,updated_at) VALUES
			('art_a1','wrk_kb_a','kb_a','Newest A','newest-a','2026-07-31T12:00:00Z'),
			('art_a2','wrk_kb_a','kb_a','Older A','older-a','2026-07-30T12:00:00Z'),
			('art_b1','wrk_kb_b','kb_b','Other B','other-b','2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Knowledgebase: knowledgebase.New(pool)}
	actor := &authorization.Actor{WorkspaceID: "wrk_kb_a", Role: "owner"}
	request := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListArticles(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/articles?limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0]["id"] != "art_a1" {
		t.Fatalf("first article page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/articles?limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0]["id"] != "art_a2" {
		t.Fatalf("second article page = %d %+v", secondResponse.Code, second)
	}
	if strings.Contains(secondResponse.Body.String(), "art_b1") {
		t.Fatal("article pagination crossed the workspace boundary")
	}
}

func TestPublicArticleSearchScopesCollection(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_kb_public','KB public','kb-public');
		INSERT INTO knowledge_bases (id,workspace_id,name,slug) VALUES ('kb_public','wrk_kb_public','Public Help','public-help');
		INSERT INTO article_collections (id,workspace_id,knowledge_base_id,name,slug) VALUES
			('col_release','wrk_kb_public','kb_public','Release notes','release-notes'),
			('col_billing','wrk_kb_public','kb_public','Billing','billing');
		INSERT INTO articles (id,workspace_id,knowledge_base_id,collection_id,title,slug,state,published_at) VALUES
			('art_release','wrk_kb_public','kb_public','col_release','Release notes','release','published',now()),
			('art_billing','wrk_kb_public','kb_public','col_billing','Billing guide','billing','published',now())
	`); err != nil {
		t.Fatal(err)
	}

	results, err := knowledgebase.New(pool).SearchPublished(ctx, "wrk_kb_public", "public-help", "release-notes", "", "", "portal", 20)
	if err != nil {
		t.Fatalf("search published articles: %v", err)
	}
	if len(results) != 1 || results[0].Article.ID != "art_release" {
		t.Fatalf("collection search results = %#v", results)
	}
}
