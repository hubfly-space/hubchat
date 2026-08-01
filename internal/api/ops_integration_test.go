//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/jobs"
)

func TestOperationalSummaryIsWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_ops_a','Ops A','ops-a'),('wrk_ops_b','Ops B','ops-b');
		INSERT INTO jobs (id,workspace_id,queue,type,state,scheduled_at) VALUES ('ops_job_a','wrk_ops_a','default','ops.a','pending',now()),('ops_job_b','wrk_ops_b','default','ops.b','dead',now());
		INSERT INTO webhook_endpoints (id,workspace_id,url,secret,auto_disabled_at) VALUES ('ops_endpoint_a','wrk_ops_a','https://a.example.test/hook','secret-a',now()),('ops_endpoint_b','wrk_ops_b','https://b.example.test/hook','secret-b',now());
		INSERT INTO webhook_deliveries (id,workspace_id,endpoint_id,event_type,payload,status) VALUES ('ops_delivery_a','wrk_ops_a','ops_endpoint_a','ticket.created','{}','pending'),('ops_delivery_b','wrk_ops_b','ops_endpoint_b','ticket.created','{}','exhausted');
		INSERT INTO files (id,workspace_id,storage_key,name,mime_type,size_bytes,committed_at) VALUES ('ops_file_a','wrk_ops_a','ops/a','a.txt','text/plain',17,now()),('ops_file_b','wrk_ops_b','ops/b','b.txt','text/plain',99,now());
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Pool: pool, Jobs: jobs.NewClient(pool), Config: config.Default()}
	actor := &authorization.Actor{WorkspaceID: "wrk_ops_a", Role: "owner"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ops/summary", nil).WithContext(authorization.WithActor(ctx, actor))
	response := httptest.NewRecorder()
	handleOpsSummary(deps)(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("summary status = %d: %s", response.Code, response.Body.String())
	}

	var summary opsSummary
	if err := json.NewDecoder(response.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.Jobs.QueueDepth != 1 || summary.Jobs.Dead != 0 || summary.Webhooks.Pending != 1 || summary.Webhooks.Exhausted != 0 || summary.Webhooks.AutoDisabled != 1 || summary.Storage.CommittedFiles != 1 || summary.Storage.Bytes != 17 || summary.Storage.PendingUploads != 0 {
		t.Fatalf("workspace A summary = %+v", summary)
	}
	if len(summary.Checks) != 5 {
		t.Fatalf("operational checks = %+v, want five checks", summary.Checks)
	}
}

func TestOperationalTestEmailQueuesForCurrentUserWorkspace(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES ('wrk_ops_email','Ops Email','ops-email');
		INSERT INTO users (id,name,email) VALUES ('usr_ops_email','Ops Owner','owner@example.test');
	`); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Email.Enabled = true
	cfg.Email.SMTPHost = "smtp.example.test"
	cfg.Email.FromAddress = "support@example.test"
	deps := Deps{Pool: pool, Jobs: jobs.NewClient(pool), Config: cfg}
	actor := &authorization.Actor{UserID: "usr_ops_email", WorkspaceID: "wrk_ops_email", Role: "owner"}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ops/test-email", nil).WithContext(authorization.WithActor(ctx, actor))
	response := httptest.NewRecorder()
	handleOpsTestEmail(deps)(response, req)
	if response.Code != http.StatusAccepted {
		t.Fatalf("test email status = %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		JobID     string `json:"job_id"`
		Recipient string `json:"recipient"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.JobID == "" || result.Recipient != "owner@example.test" || result.Status != "queued" {
		t.Fatalf("test email response = %+v", result)
	}
	var workspaceID, jobType, recipient string
	if err := pool.QueryRow(ctx, `SELECT workspace_id,type,payload->>'to' FROM jobs WHERE id=$1`, result.JobID).Scan(&workspaceID, &jobType, &recipient); err != nil {
		t.Fatal(err)
	}
	if workspaceID != actor.WorkspaceID || jobType != JobEmailSend || recipient != result.Recipient {
		t.Fatalf("queued test email job = workspace=%q type=%q recipient=%q", workspaceID, jobType, recipient)
	}
}
