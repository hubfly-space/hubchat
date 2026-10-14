//go:build integration

package webhook

import (
	"encoding/json"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/jobs"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestDeliveriesHandlesPendingNullableColumns(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_webhook_nulls','Webhook nulls','webhook-nulls');
		INSERT INTO webhook_endpoints (id,workspace_id,url,secret,events)
		VALUES ('whk_webhook_nulls','wrk_webhook_nulls','https://hooks.example.test/events','secret',ARRAY['ticket.created']);
		INSERT INTO webhook_deliveries (id,workspace_id,endpoint_id,event_type,payload)
		VALUES ('whd_webhook_nulls','wrk_webhook_nulls','whk_webhook_nulls','webhook.test','{}'::jsonb)
	`); err != nil {
		t.Fatalf("seed pending webhook delivery: %v", err)
	}

	deliveries, err := New(pool, []byte("webhook-null-regression-secret"), nil).Deliveries(ctx, "wrk_webhook_nulls", "whk_webhook_nulls", time.Time{}, "", 20)
	if err != nil {
		t.Fatalf("list pending webhook delivery: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(deliveries))
	}
	item := deliveries[0]
	if item.EventID != nil || item.ResponseStatus != nil || item.DurationMS != nil || item.DeliveredAt != nil {
		t.Fatalf("nullable delivery fields = %+v, want nil optional fields", item)
	}
}

func TestAutomationActionEnqueuesCorrectPayloadAndDeduplicates(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(id,name,slug) VALUES('wrk_auto_hook','Auto hook','auto-hook'); INSERT INTO webhook_endpoints(id,workspace_id,url,secret,events) VALUES('whk_auto_hook','wrk_auto_hook','https://example.com/hook','secret',ARRAY['ticket.created'])`); err != nil {
		t.Fatal(err)
	}
	service := New(pool, []byte("webhook-action-test-secret"), jobs.NewClient(pool))
	ctx = events.WithActionKey(ctx, "test-action")
	for range 2 {
		if err := service.EnqueueAction(ctx, "wrk_auto_hook", "whk_auto_hook", map[string]any{"order": "123"}); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var endpoint, eventType string
	var body []byte
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE workspace_id='wrk_auto_hook'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("delivery count=%d", count)
	}
	if err := pool.QueryRow(ctx, `SELECT endpoint_id,event_type,payload FROM webhook_deliveries WHERE workspace_id='wrk_auto_hook'`).Scan(&endpoint, &eventType, &body); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if endpoint != "whk_auto_hook" || eventType != "automation.action" || payload["order"] != "123" {
		t.Fatalf("incorrect delivery: %s %s %s", endpoint, eventType, body)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE workspace_id='wrk_auto_hook'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("job count=%d", count)
	}
}
