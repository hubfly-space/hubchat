//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/jobs"
)

func TestJobListAndSummaryAreWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_jobs_a','Jobs A','jobs-a'),
			('wrk_jobs_b','Jobs B','jobs-b');
		INSERT INTO jobs (id,workspace_id,queue,type,state,scheduled_at,created_at) VALUES
			('job_a1','wrk_jobs_a','default','a.newest','pending',now(), '2026-07-31T12:00:00Z'),
			('job_a2','wrk_jobs_a','default','a.failed','failed',now(), '2026-07-30T12:00:00Z'),
			('job_a3','wrk_jobs_a','default','a.running','running',now(), '2026-07-29T12:00:00Z'),
			('job_b1','wrk_jobs_b','default','b.dead','dead',now(), '2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Jobs: jobs.NewClient(pool)}
	actor := &authorization.Actor{WorkspaceID: "wrk_jobs_a", Role: "owner"}
	request := func(path string) (Page[jobs.Job], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListJobs(deps)(response, req)
		var page Page[jobs.Job]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/jobs?limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0].ID != "job_a1" {
		t.Fatalf("first job page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/jobs?limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || !second.HasMore || len(second.Data) != 1 || second.Data[0].ID != "job_a2" {
		t.Fatalf("second job page = %d %+v", secondResponse.Code, second)
	}
	if second.Data[0].WorkspaceID != actor.WorkspaceID {
		t.Fatal("job pagination crossed the workspace boundary")
	}

	requestSummary := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/summary", nil).WithContext(authorization.WithActor(ctx, actor))
	summaryResponse := httptest.NewRecorder()
	handleJobSummary(deps)(summaryResponse, requestSummary)
	if summaryResponse.Code != http.StatusOK {
		t.Fatalf("summary status = %d: %s", summaryResponse.Code, summaryResponse.Body.String())
	}
	var summary jobs.Summary
	if err := json.NewDecoder(summaryResponse.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.QueueDepth != 1 || summary.Running != 1 || summary.Failed24h != 1 || summary.Dead != 0 {
		t.Fatalf("workspace A summary = %+v", summary)
	}
}
