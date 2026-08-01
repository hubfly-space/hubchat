//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/knowledgebase"
	"github.com/hubchat/hubchat/internal/widget"
	"github.com/hubchat/hubchat/internal/workspace"
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
			('art_release_old','wrk_kb_public','kb_public','col_release','Older release notes','release-old','published',now() - interval '1 day'),
			('art_billing','wrk_kb_public','kb_public','col_billing','Billing guide','billing','published',now())
	`); err != nil {
		t.Fatal(err)
	}

	results, err := knowledgebase.New(pool).SearchPublished(ctx, "wrk_kb_public", "public-help", "release-notes", "", "", "portal", 20)
	if err != nil {
		t.Fatalf("search published articles: %v", err)
	}
	if len(results) != 2 || results[0].Article.ID != "art_release" || results[1].Article.ID != "art_release_old" {
		t.Fatalf("collection search results = %#v", results)
	}

	publicRequest := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		req.SetPathValue("workspaceID", "wrk_kb_public")
		response := httptest.NewRecorder()
		handlePublicArticleSearch(Deps{Knowledgebase: knowledgebase.New(pool)})(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}
	first, firstResponse := publicRequest("/api/v1/public/knowledge-bases/wrk_kb_public/search?knowledge_base=public-help&collection=release-notes&limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 {
		t.Fatalf("first public article page = %d %+v", firstResponse.Code, first)
	}
	firstArticle, ok := first.Data[0]["article"].(map[string]any)
	if !ok || firstArticle["id"] != "art_release" {
		t.Fatalf("first public article = %#v", first.Data[0])
	}
	second, secondResponse := publicRequest("/api/v1/public/knowledge-bases/wrk_kb_public/search?knowledge_base=public-help&collection=release-notes&limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 {
		t.Fatalf("second public article page = %d %+v", secondResponse.Code, second)
	}
	secondArticle, ok := second.Data[0]["article"].(map[string]any)
	if !ok || secondArticle["id"] != "art_release_old" {
		t.Fatalf("second public article = %#v", second.Data[0])
	}
}

func TestWidgetArticleSearchUsesDeterministicCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id,name,email,password_hash,email_verified_at)
		VALUES ($1,'Widget Search Owner',$2,'x',now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatal(err)
	}
	eventLog := events.New(pool)
	auditLog := audit.New(pool)
	wsService := workspace.New(pool, eventLog, auditLog)
	ws, err := wsService.Bootstrap(ctx, userID, "Widget Search", "widget-search")
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	actor, err := wsService.ActorForUser(ctx, ws.ID, userID)
	if err != nil {
		t.Fatalf("resolve owner: %v", err)
	}
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1`, ws.ID).Scan(&inboxID); err != nil {
		t.Fatalf("find default inbox: %v", err)
	}
	inboxService := inbox.New(pool, eventLog, auditLog)
	conversationService := conversation.New(pool, eventLog, auditLog)
	customerService := customer.New(pool, eventLog, auditLog, config.Limits{MaxEventBytes: 32 << 10, MaxAttributesPerCustomer: 100})
	widgetService := widget.New(pool, eventLog, auditLog, inboxService, conversationService, customerService, []byte("integration-widget-secret-key"))
	widgetRecord, err := widgetService.Create(ctx, ws.ID, actor.MemberID, "Support widget", &inboxID)
	if err != nil {
		t.Fatalf("create widget: %v", err)
	}
	if _, err := widgetService.AddDomain(ctx, ws.ID, actor.MemberID, widgetRecord.ID, "help.example.com"); err != nil {
		t.Fatalf("allowlist widget domain: %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO knowledge_bases (id,workspace_id,name,slug) VALUES ('kb_widget_search',$1,'Widget Help','widget-help')`, ws.ID); err != nil {
		t.Fatalf("seed widget knowledge base: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO articles (id,workspace_id,knowledge_base_id,title,slug,excerpt,body,state,language,published_at) VALUES
			('art_widget_new',$1,'kb_widget_search','Newest article','newest-article','Newest','Newest body','published','en','2026-07-31T12:00:00Z'),
			('art_widget_old',$1,'kb_widget_search','Older article','older-article','Older','Older body','published','en','2026-07-30T12:00:00Z'),
			('art_widget_other',$1,'kb_widget_search','Other article','other-article','Other','Other body','published','en','2026-07-29T12:00:00Z')
	`, ws.ID); err != nil {
		t.Fatalf("seed widget articles: %v", err)
	}

	deps := Deps{Widget: widgetService, Knowledgebase: knowledgebase.New(pool)}
	request := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		response := httptest.NewRecorder()
		handleWidgetArticleSearch(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	base := "/api/v1/widget/articles?key=" + widgetRecord.PublicKey + "&url=https%3A%2F%2Fhelp.example.com%2F"
	first, firstResponse := request(base + "&limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0]["slug"] != "newest-article" {
		t.Fatalf("first widget article page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request(base + "&limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || !second.HasMore || second.NextCursor == nil || len(second.Data) != 1 || second.Data[0]["slug"] != "older-article" {
		t.Fatalf("second widget article page = %d %+v", secondResponse.Code, second)
	}
	third, thirdResponse := request(base + "&limit=1&cursor=" + *second.NextCursor)
	if thirdResponse.Code != http.StatusOK || third.HasMore || third.NextCursor != nil || len(third.Data) != 1 || third.Data[0]["slug"] != "other-article" {
		t.Fatalf("third widget article page = %d %+v", thirdResponse.Code, third)
	}
}

func TestKnowledgeBaseAndCollectionListsUseStableCursors(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_kb_lists','KB lists','kb-lists');
		INSERT INTO knowledge_bases (id,workspace_id,name,slug,created_at) VALUES
			('kb_lists_a','wrk_kb_lists','A Help','a-help','2026-07-31T12:00:00Z'),
			('kb_lists_b','wrk_kb_lists','B Help','b-help','2026-07-31T13:00:00Z');
		INSERT INTO article_collections (id,workspace_id,knowledge_base_id,name,slug,position) VALUES
			('col_lists_a','wrk_kb_lists','kb_lists_b','First','first',1),
			('col_lists_b','wrk_kb_lists','kb_lists_b','Second','second',2)
	`); err != nil {
		t.Fatal(err)
	}
	service := knowledgebase.New(pool)
	bases, err := service.ListKnowledgeBasesPage(ctx, "wrk_kb_lists", time.Time{}, "", 2)
	if err != nil || len(bases) != 2 || bases[0].ID != "kb_lists_b" {
		t.Fatalf("knowledge-base page = %#v, err=%v", bases, err)
	}
	collections, err := service.ListCollectionsPage(ctx, "wrk_kb_lists", "kb_lists_b", 1, "col_lists_a", true, 2)
	if err != nil || len(collections) != 1 || collections[0].ID != "col_lists_b" {
		t.Fatalf("collection page = %#v, err=%v", collections, err)
	}
}
