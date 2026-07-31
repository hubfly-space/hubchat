//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/webhook"
)

func TestWebhookListUsesWorkspaceCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_hooks_a','Hooks A','hooks-a'),
			('wrk_hooks_b','Hooks B','hooks-b');
		INSERT INTO webhook_endpoints (id,workspace_id,url,secret,created_at,updated_at) VALUES
			('whk_a1','wrk_hooks_a','https://a1.example.test/hook',decode(repeat('aa',32),'hex'),'2026-07-31T12:00:00Z','2026-07-31T12:00:00Z'),
			('whk_a2','wrk_hooks_a','https://a2.example.test/hook',decode(repeat('bb',32),'hex'),'2026-07-30T12:00:00Z','2026-07-30T12:00:00Z'),
			('whk_b1','wrk_hooks_b','https://b1.example.test/hook',decode(repeat('cc',32),'hex'),'2026-07-31T13:00:00Z','2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Webhook: webhook.New(pool, []byte("01234567890123456789012345678901"), nil)}
	actor := &authorization.Actor{WorkspaceID: "wrk_hooks_a", Role: "owner"}
	request := func(path string) (Page[webhook.Endpoint], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListWebhooks(deps)(response, req)
		var page Page[webhook.Endpoint]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/webhooks?limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0].ID != "whk_a1" {
		t.Fatalf("first webhook page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/webhooks?limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0].ID != "whk_a2" {
		t.Fatalf("second webhook page = %d %+v", secondResponse.Code, second)
	}
}
