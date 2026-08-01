//go:build integration

package webhook

import (
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
