//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestWorkspaceUsageIsMeasuredAndWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_usage_a','Usage A','usage-a'),
			('wrk_usage_b','Usage B','usage-b');
		INSERT INTO customers (id,workspace_id,name,email,last_seen_at) VALUES
			('cus_usage_a','wrk_usage_a','Active A','a@example.com',now()),
			('cus_usage_b','wrk_usage_b','Active B','b@example.com',now());
		INSERT INTO files (id,workspace_id,storage_key,backend,name,mime_type,size_bytes,committed_at)
		VALUES ('fil_usage_a','wrk_usage_a','wrk_usage_a/fil_usage_a','local','a.txt','text/plain',42,now()),
		       ('fil_usage_b','wrk_usage_b','wrk_usage_b/fil_usage_b','local','b.txt','text/plain',99,now())
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Pool: pool, Analytics: analytics.New(pool), Config: config.Default()}
	actor := &authorization.Actor{WorkspaceID: "wrk_usage_a", Role: "owner"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspace/usage", nil).WithContext(authorization.WithActor(ctx, actor))
	response := httptest.NewRecorder()
	handleGetUsage(deps)(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("usage status = %d: %s", response.Code, response.Body.String())
	}
	var snapshot struct {
		Metrics []struct {
			Key  string `json:"key"`
			Used *int64 `json:"used"`
		} `json:"metrics"`
		ComputedAt time.Time `json:"computed_at"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.ComputedAt.IsZero() {
		t.Fatal("usage response did not include computed_at")
	}
	seenCustomer, seenStorage := false, false
	for _, metric := range snapshot.Metrics {
		switch metric.Key {
		case "monthly_active_contacts":
			seenCustomer = true
			if metric.Used == nil || *metric.Used != 1 {
				t.Fatalf("workspace A customer usage = %+v", metric.Used)
			}
		case "storage_bytes":
			seenStorage = true
			if metric.Used == nil || *metric.Used != 42 {
				t.Fatalf("workspace A storage usage = %+v", metric.Used)
			}
		}
	}
	if !seenCustomer || !seenStorage {
		t.Fatalf("usage response omitted expected metrics: %+v", snapshot.Metrics)
	}
}
