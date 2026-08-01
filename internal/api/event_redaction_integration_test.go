//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestEventStreamRedactsPayloadForAgentsWithoutSensitiveCapability(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_event_redact','Event Redact','event-redact');
		INSERT INTO customers (id,workspace_id,name) VALUES ('cus_event_redact','wrk_event_redact','Ada');
		INSERT INTO customer_events (id,workspace_id,customer_id,type,source,payload) VALUES
			('evt_event_redact','wrk_event_redact','cus_event_redact','checkout.started','js_sdk',
			 '{"order_id":"ord_1","email":"ada@example.com","profile":{"phone_number":"+250780000000","plan":"pro"}}');
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Customer: customer.New(pool, nil, nil, config.Default().Limits)}
	request := func(actor *authorization.Actor) map[string]any {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=10", nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListEvents(deps)(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("event list status = %d: %s", response.Code, response.Body.String())
		}
		var page struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		if len(page.Data) != 1 {
			t.Fatalf("event page = %#v", page.Data)
		}
		return page.Data[0]
	}

	agentEvent := request(&authorization.Actor{
		WorkspaceID:  "wrk_event_redact",
		Role:         "agent",
		Capabilities: map[authorization.Capability]bool{authorization.CustomerRead: true},
	})
	agentPayload := agentEvent["payload"].(map[string]any)
	if agentPayload["email"] != "[REDACTED]" {
		t.Fatalf("agent email = %#v", agentPayload["email"])
	}
	agentProfile := agentPayload["profile"].(map[string]any)
	if agentProfile["phone_number"] != "[REDACTED]" || agentProfile["plan"] != "pro" {
		t.Fatalf("agent nested payload = %#v", agentProfile)
	}

	privilegedEvent := request(&authorization.Actor{
		WorkspaceID:  "wrk_event_redact",
		Role:         "owner",
		Capabilities: map[authorization.Capability]bool{authorization.CustomerReadSensitive: true},
	})
	privilegedPayload := privilegedEvent["payload"].(map[string]any)
	if privilegedPayload["email"] != "ada@example.com" {
		t.Fatalf("privileged email = %#v", privilegedPayload["email"])
	}
}
