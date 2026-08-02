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

func TestRollupsPageUsesDimensionTieBreaker(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_analytics_page','Analytics page','analytics-page')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO report_rollups (id,workspace_id,metric,grain,bucket,dimensions,value,count)
		VALUES
			('rup_email','wrk_analytics_page','conversations.created','day','2026-08-01T00:00:00Z','{"channel":"email"}',1,1),
			('rup_widget','wrk_analytics_page','conversations.created','day','2026-08-01T00:00:00Z','{"channel":"widget"}',2,2),
			('rup_next_day','wrk_analytics_page','conversations.created','day','2026-08-02T00:00:00Z','{"channel":"email"}',3,3)
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	first, err := service.RollupsPage(ctx, "wrk_analytics_page", "conversations.created", "day", time.Time{}, time.Time{}, time.Time{}, "", 2)
	if err != nil || len(first) != 2 || first[0].CursorKey == "" || first[1].CursorKey == "" {
		t.Fatalf("first rollup page = %+v, err=%v", first, err)
	}
	second, err := service.RollupsPage(ctx, "wrk_analytics_page", "conversations.created", "day", time.Time{}, time.Time{}, first[1].Bucket, first[1].CursorKey, 2)
	if err != nil || len(second) != 1 || !second[0].Bucket.After(first[1].Bucket) {
		t.Fatalf("second rollup page = %+v, err=%v", second, err)
	}

	all, err := service.Rollups(ctx, "wrk_analytics_page", "conversations.created", "day", time.Time{}, time.Time{})
	if err != nil || len(all) != 3 {
		t.Fatalf("full rollups = %+v, err=%v", all, err)
	}
}

func TestNoResultSearchesPageUsesStableCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug)
		VALUES ('wrk_analytics_search_page','Analytics search page','analytics-search-page')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO article_searches (id,workspace_id,query,result_count,surface,occurred_at)
		VALUES
			('ase_billing_1','wrk_analytics_search_page','billing',0,'portal','2026-08-01T10:00:00Z'),
			('ase_billing_2','wrk_analytics_search_page','billing',0,'widget','2026-08-02T10:00:00Z'),
			('ase_password','wrk_analytics_search_page','password reset',0,'portal','2026-08-03T10:00:00Z'),
			('ase_found','wrk_analytics_search_page','billing',2,'portal','2026-08-04T10:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	from, to := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	first, err := service.NoResultSearchesPage(ctx, "wrk_analytics_search_page", from, to, 0, time.Time{}, "", 1)
	if err != nil || len(first) != 1 || first[0].Query != "billing" || first[0].Count != 2 {
		t.Fatalf("first no-result search page = %+v, err=%v", first, err)
	}
	second, err := service.NoResultSearchesPage(ctx, "wrk_analytics_search_page", from, to, first[0].Count, first[0].LastOccurredAt, first[0].Query, 1)
	if err != nil || len(second) != 1 || second[0].Query != "password reset" || second[0].Count != 1 {
		t.Fatalf("second no-result search page = %+v, err=%v", second, err)
	}
}

func TestFoldWorkspacePromotesKnowledgeBaseAndFeedbackEvents(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_analytics_content','Analytics content','analytics-content')`); err != nil {
		t.Fatal(err)
	}

	log := events.New(pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []events.Event{
		{WorkspaceID: "wrk_analytics_content", Type: events.ArticleViewed, EntityType: "article", EntityID: "art_content", ActorType: events.ActorVisitor, Data: map[string]any{"surface": "portal"}},
		{WorkspaceID: "wrk_analytics_content", Type: events.ArticleSearchRecorded, EntityType: "article_search", ActorType: events.ActorVisitor, Data: map[string]any{"surface": "portal", "result_count": 0}},
		{WorkspaceID: "wrk_analytics_content", Type: events.ArticleFeedbackRecorded, EntityType: "article", EntityID: "art_content", ActorType: events.ActorVisitor, Data: map[string]any{"helpful": true}},
		{WorkspaceID: "wrk_analytics_content", Type: events.FeedbackVoteRecorded, EntityType: "feedback_item", EntityID: "fdb_content", ActorType: events.ActorCustomer, Data: map[string]any{}},
		{WorkspaceID: "wrk_analytics_content", Type: events.FeedbackStatusChanged, EntityType: "feedback_item", EntityID: "fdb_content", ActorType: events.ActorUser, Data: map[string]any{"to": "planned"}},
	} {
		if _, err := log.Append(ctx, tx, event); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	service := New(pool)
	if folded, err := service.FoldWorkspace(ctx, "wrk_analytics_content", time.Now().UTC()); err != nil {
		t.Fatal(err)
	} else if folded != 6 {
		t.Fatalf("folded content event metrics = %d, want 6", folded)
	}
	for _, metric := range []string{
		"knowledgebase.article_views",
		"knowledgebase.searches",
		"knowledgebase.search_no_results",
		"knowledgebase.article_helpful",
		"feedback.votes",
		"feedback.status_changed",
	} {
		rollups, err := service.Rollups(ctx, "wrk_analytics_content", metric, "day", time.Now().UTC().AddDate(0, 0, -1), time.Now().UTC().AddDate(0, 0, 1))
		if err != nil {
			t.Fatalf("rollup %s: %v", metric, err)
		}
		if len(rollups) != 1 || rollups[0].Value != 1 {
			t.Fatalf("rollup %s = %+v, want one value", metric, rollups)
		}
	}
}

func TestSummaryComputesFirstAndNextResponseTimes(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	base := time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_analytics_response','Analytics response','analytics-response')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inboxes (id,workspace_id,name,slug) VALUES ('inb_analytics_response','wrk_analytics_response','Support','support')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations (id,workspace_id,inbox_id,channel,created_at) VALUES ('cnv_analytics_response','wrk_analytics_response','inb_analytics_response','manual',$1)`, base); err != nil {
		t.Fatal(err)
	}
	for _, message := range []struct {
		id         string
		authorType string
		sequence   int
		at         time.Time
	}{
		{"msg_analytics_customer_one", "customer", 1, base.Add(1 * time.Minute)},
		{"msg_analytics_agent_one", "agent", 2, base.Add(3 * time.Minute)},
		{"msg_analytics_customer_two", "customer", 3, base.Add(10 * time.Minute)},
		{"msg_analytics_agent_two", "agent", 4, base.Add(15 * time.Minute)},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO messages (id,workspace_id,conversation_id,author_type,author_name,sequence,created_at) VALUES ($1,'wrk_analytics_response','cnv_analytics_response',$2,$2,$3,$4)`, message.id, message.authorType, message.sequence, message.at); err != nil {
			t.Fatal(err)
		}
	}

	result, err := New(pool).Summary(ctx, "wrk_analytics_response", base, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.FirstResponseSeconds != 120 {
		t.Fatalf("first response = %v, want 120 seconds", result.FirstResponseSeconds)
	}
	if result.NextResponseSeconds != 210 {
		t.Fatalf("next response = %v, want 210 seconds", result.NextResponseSeconds)
	}
}

func TestWorkloadIsWorkspaceScopedAndCountsCurrentAssignments(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	base := time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO workspaces (id,name,slug) VALUES ('wrk_workload_a','Workload A','workload-a'),('wrk_workload_b','Workload B','workload-b')`, nil},
		{`INSERT INTO users (id,name,email) VALUES ('usr_workload_a','Agent A','workload-a@example.com')`, nil},
		{`INSERT INTO workspace_members (id,workspace_id,user_id,role) VALUES ('mem_workload_a','wrk_workload_a','usr_workload_a','agent')`, nil},
		{`INSERT INTO teams (id,workspace_id,name) VALUES ('team_workload_a','wrk_workload_a','Support')`, nil},
		{`INSERT INTO team_members (team_id,member_id) VALUES ('team_workload_a','mem_workload_a')`, nil},
		{`INSERT INTO inboxes (id,workspace_id,name,slug) VALUES ('inb_workload_a','wrk_workload_a','Support','support')`, nil},
		{`INSERT INTO conversations (id,workspace_id,inbox_id,channel,assignee_id,team_id,state,created_at) VALUES ('cnv_workload_a','wrk_workload_a','inb_workload_a','manual','mem_workload_a','team_workload_a','open',$1)`, []any{base}},
		{`INSERT INTO tickets (id,workspace_id,number,prefix,title,assignee_id,team_id,created_at,updated_at) VALUES ('tkt_workload_a','wrk_workload_a',1,'SUP','Login issue','mem_workload_a','team_workload_a',$1,$1)`, []any{base}},
		{`INSERT INTO messages (id,workspace_id,conversation_id,author_type,author_id,author_name,sequence,created_at) VALUES ('msg_workload_a','wrk_workload_a','cnv_workload_a','agent','mem_workload_a','Agent A',1,$1)`, []any{base}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := New(pool).Workload(ctx, "wrk_workload_a", base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("workload rows = %+v, want member and team", rows)
	}
	for _, row := range rows {
		if row.SubjectType == "member" && (row.ActiveConversations != 1 || row.ActiveTickets != 1 || row.RepliesSent != 1) {
			t.Fatalf("member workload = %+v", row)
		}
		if row.SubjectType == "team" && (row.ActiveConversations != 1 || row.ActiveTickets != 1 || row.RepliesSent != 1) {
			t.Fatalf("team workload = %+v", row)
		}
	}
	foreign, err := New(pool).Workload(ctx, "wrk_workload_b", base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(foreign) != 0 {
		t.Fatalf("foreign workload = %+v", foreign)
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
		Name: "Daily volume", Definition: map[string]any{"metrics": []string{"conversations.created"}}, DateRange: "last_30_days", Timezone: "Africa/Kigali",
	})
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := service.CreateSchedule(ctx, "wrk_schedule_one", ScheduleInput{
		ReportID: report.ID, Cadence: "daily", Recipients: []string{"ops@example.com", "alerts@example.com"}, Options: map[string]any{"hour": 9, "minute": 0},
	}, time.Date(2026, time.July, 31, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if schedule.Options["timezone"] != "Africa/Kigali" {
		t.Fatalf("schedule timezone = %#v, want report timezone", schedule.Options["timezone"])
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
	if err != nil || processed != 1 || len(queue.specs) != 2 {
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

func TestSavedReportsSupportVisibilityUpdateDeleteAndWorkspaceIsolation(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	for _, workspace := range []struct{ id, name, slug string }{
		{"wrk_report_access_a", "Report access A", "report-access-a"},
		{"wrk_report_access_b", "Report access B", "report-access-b"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ($1,$2,$3)`, workspace.id, workspace.name, workspace.slug); err != nil {
			t.Fatal(err)
		}
	}
	service := New(pool)
	private, err := service.CreateReport(ctx, "wrk_report_access_a", "", ReportInput{
		Name: "Private operations", Definition: map[string]any{"metrics": []string{"tickets.created"}}, VisibleToRoles: []string{"admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	public, err := service.CreateReport(ctx, "wrk_report_access_a", "", ReportInput{Name: "Public operations"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetReportForActor(ctx, "wrk_report_access_a", "agent", "mem_other", private.ID); !errors.Is(err, ErrReportForbidden) {
		t.Fatalf("agent private report error = %v, want ErrReportForbidden", err)
	}
	visible, err := service.ListReportsPageForActor(ctx, "wrk_report_access_a", "agent", "mem_other", "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].ID != public.ID {
		t.Fatalf("agent-visible reports = %+v, want public report only", visible)
	}
	if _, err := service.UpdateReport(ctx, "wrk_report_access_a", public.ID, ReportInput{Name: "Renamed operations", Description: "Updated", VisibleToRoles: []string{"manager"}}); err != nil {
		t.Fatal(err)
	}
	updated, err := service.GetReport(ctx, "wrk_report_access_a", public.ID)
	if err != nil || updated.Name != "Renamed operations" || len(updated.VisibleToRoles) != 1 || updated.VisibleToRoles[0] != "manager" {
		t.Fatalf("updated report = %+v, err=%v", updated, err)
	}
	if _, err := service.UpdateReport(ctx, "wrk_report_access_b", public.ID, ReportInput{Name: "Foreign update"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign update error = %v, want pgx.ErrNoRows", err)
	}
	if err := service.DeleteReport(ctx, "wrk_report_access_b", public.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign delete error = %v, want pgx.ErrNoRows", err)
	}
	if err := service.DeleteReport(ctx, "wrk_report_access_a", public.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetReport(ctx, "wrk_report_access_a", public.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted report lookup error = %v, want pgx.ErrNoRows", err)
	}
}
