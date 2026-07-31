//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/feedback"
)

func TestFeedbackItemsUseCompositeVoteCursorAndWorkspaceScope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_feedback_a','Feedback A','feedback-a'),
			('wrk_feedback_b','Feedback B','feedback-b');
		INSERT INTO feedback_boards (id,workspace_id,name,slug) VALUES
			('brd_feedback_a','wrk_feedback_a','A Board','a-board'),
			('brd_feedback_b','wrk_feedback_b','B Board','b-board');
		INSERT INTO feedback_items (id,workspace_id,board_id,title,vote_count,created_at) VALUES
			('fbi_a1','wrk_feedback_a','brd_feedback_a','Most voted',10,'2026-07-31T12:00:00Z'),
			('fbi_a2','wrk_feedback_a','brd_feedback_a','Second voted',5,'2026-07-30T12:00:00Z'),
			('fbi_b1','wrk_feedback_b','brd_feedback_b','Other workspace',99,'2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Feedback: feedback.New(pool, nil, nil)}
	actor := &authorization.Actor{WorkspaceID: "wrk_feedback_a", Role: "owner"}
	request := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		req.SetPathValue("id", "brd_feedback_a")
		response := httptest.NewRecorder()
		handleListFeedbackItems(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/feedback/boards/brd_feedback_a/items?limit=1&sort=votes")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0]["id"] != "fbi_a1" {
		t.Fatalf("first feedback page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/feedback/boards/brd_feedback_a/items?limit=1&sort=votes&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0]["id"] != "fbi_a2" {
		t.Fatalf("second feedback page = %d %+v", secondResponse.Code, second)
	}
}
