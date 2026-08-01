//go:build integration

package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/jackc/pgx/v5"
)

type scheduleQueue struct{ specs []jobs.Spec }

func (q *scheduleQueue) Enqueue(_ context.Context, spec jobs.Spec) (string, error) {
	q.specs = append(q.specs, spec)
	return "job_test", nil
}

func TestFoldWorkspacePromotesWidgetSurfaceEvents(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_analytics_surface','Analytics surface','analytics-surface')`); err != nil {
		t.Fatal(err)
	}

	log := events.New(pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(ctx, tx, events.Event{
		WorkspaceID: "wrk_analytics_surface",
		Type:        events.EventReceived,
		EntityType:  "customer",
		ActorType:   events.ActorVisitor,
		Data:        map[string]any{"type": "widget.impression", "source": "js_sdk"},
	}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	if folded, err := service.FoldWorkspace(ctx, "wrk_analytics_surface", time.Now().UTC()); err != nil {
		t.Fatal(err)
	} else if folded != 1 {
		t.Fatalf("folded event count = %d, want 1", folded)
	}

	rollups, err := service.Rollups(ctx, "wrk_analytics_surface", "surfaces.widget.impressions", "day", time.Now().UTC().AddDate(0, 0, -1), time.Now().UTC().AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(rollups) != 1 || rollups[0].Value != 1 {
		t.Fatalf("surface rollups = %+v, want one impression", rollups)
	}
}

func TestReportSchedulesAreWorkspaceScopedAndClaimedOnce(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	for _, workspace := range []struct{ id, name, slug string }{
		{"wrk_schedule_one", "Schedule One", "schedule-one"},
		{"wrk_schedule_two", "Schedule Two", "schedule-two"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ($1,$2,$3)`, workspace.id, workspace.name, workspace.slug); err != nil {
			t.Fatal(err)
		}
	}
	service := New(pool)
	report, err := service.CreateReport(ctx, "wrk_schedule_one", "", ReportInput{
		Name: "Daily volume", Definition: map[string]any{"metrics": []string{"conversations.created"}}, DateRange: "last_30_days",
	})
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := service.CreateSchedule(ctx, "wrk_schedule_one", ScheduleInput{
		ReportID: report.ID, Cadence: "daily", Recipients: []string{"ops@example.com"}, Options: map[string]any{"hour": 9, "minute": 0},
	}, time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetSchedule(ctx, "wrk_schedule_two", schedule.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-workspace schedule lookup error = %v", err)
	}
	var due time.Time
	if err := pool.QueryRow(ctx, `UPDATE report_schedules SET next_run_at=$3 WHERE workspace_id=$1 AND id=$2 RETURNING next_run_at`, "wrk_schedule_one", schedule.ID, time.Now().UTC().Add(-time.Minute)).Scan(&due); err != nil {
		t.Fatal(err)
	}
	queue := &scheduleQueue{}
	processed, err := service.RunScheduledReports(ctx, time.Now().UTC(), queue)
	if err != nil || processed != 1 || len(queue.specs) != 1 {
		t.Fatalf("scheduled reports processed=%d jobs=%d err=%v", processed, len(queue.specs), err)
	}
	processed, err = service.RunScheduledReports(ctx, time.Now().UTC(), queue)
	if err != nil || processed != 0 {
		t.Fatalf("second scheduled report run processed=%d err=%v", processed, err)
	}
}

func TestListReportsPageIsWorkspaceScopedAndStable(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	for _, workspace := range []struct{ id, name, slug string }{
		{"wrk_reports_page_a", "Reports A", "reports-page-a"},
		{"wrk_reports_page_b", "Reports B", "reports-page-b"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ($1,$2,$3)`, workspace.id, workspace.name, workspace.slug); err != nil {
			t.Fatal(err)
		}
	}
	service := New(pool)
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		if _, err := service.CreateReport(ctx, "wrk_reports_page_a", "", ReportInput{Name: name, Definition: map[string]any{"metrics": []string{"tickets.created"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.CreateReport(ctx, "wrk_reports_page_b", "", ReportInput{Name: "Other workspace", Definition: map[string]any{}}); err != nil {
		t.Fatal(err)
	}

	first, err := service.ListReportsPage(ctx, "wrk_reports_page_a", "", "", 3)
	if err != nil || len(first) != 3 {
		t.Fatalf("first report page = %d rows, err=%v", len(first), err)
	}
	second, err := service.ListReportsPage(ctx, "wrk_reports_page_a", first[1].Name, first[1].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Name != "Gamma" || second[0].WorkspaceID != "wrk_reports_page_a" {
		t.Fatalf("second report page = %+v", second)
	}
}
