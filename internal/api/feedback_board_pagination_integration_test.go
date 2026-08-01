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

func TestFeedbackBoardListUsesPositionCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_boards_a','Boards A','boards-a'),
			('wrk_boards_b','Boards B','boards-b');
		INSERT INTO feedback_boards (id,workspace_id,name,slug,position) VALUES
			('brd_a1','wrk_boards_a','First A','first-a',1),
			('brd_a2','wrk_boards_a','Second A','second-a',2),
			('brd_b1','wrk_boards_b','Other B','other-b',1)
		;
		UPDATE feedback_boards SET visibility='private' WHERE id='brd_a2'
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Feedback: feedback.New(pool, nil, nil)}
	actor := &authorization.Actor{WorkspaceID: "wrk_boards_a", Role: "owner"}
	request := func(path string) (Page[feedback.Board], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListFeedbackBoards(deps)(response, req)
		var page Page[feedback.Board]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/feedback/boards?limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0].ID != "brd_a1" {
		t.Fatalf("first board page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/feedback/boards?limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0].ID != "brd_a2" {
		t.Fatalf("second board page = %d %+v", secondResponse.Code, second)
	}

	publicRequest := func(path string) (Page[feedback.Board], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		req.SetPathValue("workspaceID", "wrk_boards_a")
		response := httptest.NewRecorder()
		handlePublicFeedbackBoards(deps)(response, req)
		var page Page[feedback.Board]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}
	public, publicResponse := publicRequest("/api/v1/public/feedback/wrk_boards_a/boards?limit=2")
	if publicResponse.Code != http.StatusOK || public.HasMore || len(public.Data) != 1 || public.Data[0].ID != "brd_a1" || public.Data[0].Visibility != "public" {
		t.Fatalf("public board page = %d %+v", publicResponse.Code, public)
	}
}
