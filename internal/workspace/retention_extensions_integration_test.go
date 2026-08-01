//go:build integration

package workspace_test

import (
	"context"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/survey"
	"github.com/hubchat/hubchat/internal/webhook"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestRetentionSweepCoversWebhookSurveyAndAuditCategories(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	workspaceService := newTestService(t, pool)
	workspaceA, _, memberA := seedOwnerWorkspace(t, ctx, pool, workspaceService, "retention-extra-a@example.com")
	workspaceB, _, _ := seedOwnerWorkspace(t, ctx, pool, workspaceService, "retention-extra-b@example.com")
	setExtendedRetention(t, ctx, pool, workspaceA)
	setExtendedRetention(t, ctx, pool, workspaceB)
	seedExtendedRetentionRows(t, ctx, pool, workspaceA, "a")
	seedExtendedRetentionRows(t, ctx, pool, workspaceB, "b")

	hold, err := workspaceService.CreateLegalHold(ctx, workspaceA, memberA, workspace.LegalHoldInput{Category: "all", Reason: "Preserve all operational history"})
	if err != nil {
		t.Fatalf("create all-category hold: %v", err)
	}

	webhooks := webhook.New(pool, []byte("retention-test-secret"), nil)
	surveys := survey.New(pool)
	auditLog := audit.New(pool)
	webhooksDeleted, err := webhooks.RetentionSweep(ctx)
	if err != nil {
		t.Fatalf("webhook retention sweep: %v", err)
	}
	surveysDeleted, err := surveys.RetentionSweep(ctx)
	if err != nil {
		t.Fatalf("survey retention sweep: %v", err)
	}
	auditDeleted, err := auditLog.RetentionSweep(ctx)
	if err != nil {
		t.Fatalf("audit retention sweep: %v", err)
	}
	if webhooksDeleted != 1 || surveysDeleted != 1 || auditDeleted != 1 {
		t.Fatalf("expected workspace B rows only to be deleted, got webhooks=%d surveys=%d audit=%d", webhooksDeleted, surveysDeleted, auditDeleted)
	}

	if _, err := workspaceService.ReleaseLegalHold(ctx, workspaceA, memberA, hold.ID); err != nil {
		t.Fatalf("release all-category hold: %v", err)
	}
	webhooksDeleted, err = webhooks.RetentionSweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	surveysDeleted, err = surveys.RetentionSweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	auditDeleted, err = auditLog.RetentionSweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if webhooksDeleted != 1 || surveysDeleted != 1 || auditDeleted != 1 {
		t.Fatalf("expected workspace A rows to delete after release, got webhooks=%d surveys=%d audit=%d", webhooksDeleted, surveysDeleted, auditDeleted)
	}
}

func setExtendedRetention(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		UPDATE workspaces SET settings = '{"privacy":{"retention_days":{"webhook_deliveries":7,"survey_responses":7,"audit_logs":7}}}'::jsonb
		WHERE id = $1
	`, workspaceID); err != nil {
		t.Fatalf("set extended retention: %v", err)
	}
}

func seedExtendedRetentionRows(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID, suffix string) {
	t.Helper()
	endpointID := "whk_retention_" + suffix
	deliveryID := "whd_retention_" + suffix
	if _, err := pool.Exec(ctx, `
		INSERT INTO webhook_endpoints (id,workspace_id,url,secret,events)
		VALUES ($1,$2,'https://example.test/hook','retention-secret','{}')
	`, endpointID, workspaceID); err != nil {
		t.Fatalf("seed webhook endpoint: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO webhook_deliveries (id,workspace_id,endpoint_id,event_type,payload,status,created_at)
		VALUES ($1,$2,$3,'ticket.created','{}','delivered',now()-interval '30 days')
	`, deliveryID, workspaceID, endpointID); err != nil {
		t.Fatalf("seed webhook delivery: %v", err)
	}

	surveyID := "svy_retention_" + suffix
	responseID := "srp_retention_" + suffix
	if _, err := pool.Exec(ctx, `INSERT INTO surveys (id,workspace_id,name,type) VALUES ($1,$2,'Retention survey','csat')`, surveyID, workspaceID); err != nil {
		t.Fatalf("seed survey: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO survey_responses (id,workspace_id,survey_id,score,submitted_at)
		VALUES ($1,$2,$3,5,now()-interval '30 days')
	`, responseID, workspaceID, surveyID); err != nil {
		t.Fatalf("seed survey response: %v", err)
	}

	auditID := ids.New(ids.PrefixAuditLog)
	if _, err := pool.Exec(ctx, `
		INSERT INTO audit_logs (id,workspace_id,actor_type,action,metadata,occurred_at)
		VALUES ($1,$2,'system','retention.test','{}',now()-interval '30 days')
	`, auditID, workspaceID); err != nil {
		t.Fatalf("seed audit row: %v", err)
	}

}
