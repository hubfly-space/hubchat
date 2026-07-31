//go:build integration

package survey

import (
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

func TestSubmitPublishesEventAndAnalyticsSummaryIncludesCSAT(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	workspaceID := ids.New(ids.PrefixWorkspace)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ($1,'Survey analytics','survey-analytics')`, workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	customerID := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id,workspace_id,name,email) VALUES ($1,$2,'Survey customer','survey@example.com')`, customerID, workspaceID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	service := New(pool, Options{Events: events.New(pool)})
	created, err := service.Create(ctx, workspaceID, Input{
		Name:      "Post-resolution CSAT",
		Type:      "csat",
		Questions: []QuestionInput{{Prompt: "How was the support?", Type: "number", Required: true}},
	})
	if err != nil {
		t.Fatalf("create survey: %v", err)
	}
	questionID := created.Questions[0].ID
	if _, err := service.Submit(ctx, workspaceID, created.ID, customerID, ResponseInput{
		Answers: map[string]any{questionID: float64(4)},
	}); err != nil {
		t.Fatalf("submit survey: %v", err)
	}

	var eventType string
	var score float64
	if err := pool.QueryRow(ctx, `SELECT type,(data->>'score')::double precision FROM workspace_events WHERE workspace_id=$1 AND type=$2`, workspaceID, events.SurveyResponseCreated).Scan(&eventType, &score); err != nil {
		t.Fatalf("load survey event: %v", err)
	}
	if eventType != string(events.SurveyResponseCreated) || score != 4 {
		t.Fatalf("survey event = type %q score %v", eventType, score)
	}

	from := time.Now().UTC().Add(-time.Minute)
	summary, err := analytics.New(pool).Summary(ctx, workspaceID, from, time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("analytics summary: %v", err)
	}
	if summary.SurveyResponses != 1 || summary.CSATAverage == nil || *summary.CSATAverage != 4 {
		t.Fatalf("survey summary = responses %d csat %v", summary.SurveyResponses, summary.CSATAverage)
	}
	analyticsService := analytics.New(pool)
	if count, err := analyticsService.FoldWorkspace(ctx, workspaceID, time.Now().UTC()); err != nil {
		t.Fatalf("fold survey event: %v", err)
	} else if count != 1 {
		t.Fatalf("folded survey event count = %d, want 1", count)
	}
	today := time.Now().UTC()
	dayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	rollups, err := analyticsService.Rollups(ctx, workspaceID, "surveys.responses", "day", dayStart, dayStart.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("load survey rollup: %v", err)
	}
	if len(rollups) != 1 || rollups[0].Value != 1 || rollups[0].Count != 1 {
		t.Fatalf("survey rollups = %#v", rollups)
	}
}
