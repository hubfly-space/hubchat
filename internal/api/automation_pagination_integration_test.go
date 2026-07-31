//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/automation"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestAutomationRuleListUsesExecutionOrderCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_rules_a','Rules A','rules-a'),
			('wrk_rules_b','Rules B','rules-b');
		INSERT INTO automation_rules (id,workspace_id,name,trigger,position,created_at,updated_at) VALUES
			('rule_a1','wrk_rules_a','First A','conversation.created',1,'2026-07-31T12:00:00Z','2026-07-31T12:00:00Z'),
			('rule_a2','wrk_rules_a','Second A','conversation.created',2,'2026-07-30T12:00:00Z','2026-07-30T12:00:00Z'),
			('rule_b1','wrk_rules_b','Other B','conversation.created',1,'2026-07-31T13:00:00Z','2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Automation: automation.New(pool)}
	actor := &authorization.Actor{WorkspaceID: "wrk_rules_a", Role: "owner"}
	request := func(path string) (Page[automation.Rule], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListAutomationRules(deps)(response, req)
		var page Page[automation.Rule]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/automation/rules?limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0].ID != "rule_a1" {
		t.Fatalf("first rule page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/automation/rules?limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0].ID != "rule_a2" {
		t.Fatalf("second rule page = %d %+v", secondResponse.Code, second)
	}
}
