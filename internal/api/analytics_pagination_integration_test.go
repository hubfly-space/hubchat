//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestAnalyticsRollupsEndpointUsesCursorPage(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_api_rollups','API rollups','api-rollups')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO report_rollups (id,workspace_id,metric,grain,bucket,dimensions,value,count)
		VALUES
			('rap_first','wrk_api_rollups','conversations.created','day','2026-08-01T00:00:00Z','{}',1,1),
			('rap_second','wrk_api_rollups','conversations.created','day','2026-08-02T00:00:00Z','{}',2,2)
	`); err != nil {
		t.Fatal(err)
	}

	actor := &authorization.Actor{WorkspaceID: "wrk_api_rollups", Role: "owner", MemberID: "mem_api_rollups"}
	deps := Deps{Analytics: analytics.New(pool)}
	request := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListAnalyticsRollups(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	base := "/api/v1/analytics/rollups?metric=conversations.created&grain=day&from=2026-08-01T00:00:00Z&to=2026-08-03T00:00:00Z&timezone=Africa%2FKigali"
	first, firstResponse := request(base + "&limit=1")
	if firstResponse.Code != http.StatusOK || firstResponse.Header().Get("X-Hubchat-Analytics-Timezone") != "Africa/Kigali" || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 {
		t.Fatalf("first analytics rollup page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request(base + "&limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || second.NextCursor != nil || len(second.Data) != 1 {
		t.Fatalf("second analytics rollup page = %d %+v", secondResponse.Code, second)
	}
	if first.Data[0]["bucket"] == second.Data[0]["bucket"] {
		t.Fatal("analytics rollup cursor repeated the first bucket")
	}
}

func TestAnalyticsNoResultSearchesEndpointUsesCursorPage(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_api_searches','API searches','api-searches')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO article_searches (id,workspace_id,query,result_count,surface,occurred_at)
		VALUES
			('aas_billing_1','wrk_api_searches','billing',0,'portal','2026-08-01T10:00:00Z'),
			('aas_billing_2','wrk_api_searches','billing',0,'widget','2026-08-02T10:00:00Z'),
			('aas_password','wrk_api_searches','password reset',0,'portal','2026-08-03T10:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	actor := &authorization.Actor{WorkspaceID: "wrk_api_searches", Role: "owner", MemberID: "mem_api_searches"}
	deps := Deps{Analytics: analytics.New(pool)}
	request := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListNoResultSearches(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	base := "/api/v1/analytics/searches/no-results?from=2026-08-01T00:00:00Z&to=2026-08-04T00:00:00Z"
	first, firstResponse := request(base + "&limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0]["query"] != "billing" {
		t.Fatalf("first no-result search page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request(base + "&limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || second.NextCursor != nil || len(second.Data) != 1 || second.Data[0]["query"] != "password reset" {
		t.Fatalf("second no-result search page = %d %+v", secondResponse.Code, second)
	}
}
