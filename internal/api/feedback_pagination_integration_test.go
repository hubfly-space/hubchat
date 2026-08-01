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
		INSERT INTO inboxes (id,workspace_id,name,slug) VALUES
			('inb_feedback_a','wrk_feedback_a','A Inbox','a-inbox');
		INSERT INTO conversations (id,workspace_id,inbox_id,channel,subject) VALUES
			('conv_feedback_a','wrk_feedback_a','inb_feedback_a','manual','Feedback conversation');
		INSERT INTO feedback_items (id,workspace_id,board_id,title,vote_count,created_at) VALUES
			('fbi_a1','wrk_feedback_a','brd_feedback_a','Most voted',10,'2026-07-31T12:00:00Z'),
			('fbi_a2','wrk_feedback_a','brd_feedback_a','Second voted',5,'2026-07-30T12:00:00Z'),
			('fbi_b1','wrk_feedback_b','brd_feedback_b','Other workspace',99,'2026-07-31T13:00:00Z')
		;
		INSERT INTO feedback_links (id,workspace_id,item_id,conversation_id) VALUES
			('fbl_feedback_a','wrk_feedback_a','fbi_a1','conv_feedback_a');
		INSERT INTO feedback_comments (id,workspace_id,item_id,author_type,author_name,body,created_at) VALUES
			('comment_a1','wrk_feedback_a','fbi_a1','customer','A customer','First comment','2026-07-30T12:00:00Z'),
			('comment_a2','wrk_feedback_a','fbi_a1','agent','A teammate','Second comment','2026-07-31T12:00:00Z'),
			('comment_b1','wrk_feedback_b','fbi_b1','customer','B customer','Other workspace','2026-07-31T13:00:00Z')
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
	allItemsRequest := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListAllFeedbackItems(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}
	allFirst, allFirstResponse := allItemsRequest("/api/v1/feedback/items?limit=1&q=voted")
	if allFirstResponse.Code != http.StatusOK || !allFirst.HasMore || allFirst.NextCursor == nil || len(allFirst.Data) != 1 || allFirst.Data[0]["id"] != "fbi_a1" {
		t.Fatalf("first workspace feedback search = %d %+v", allFirstResponse.Code, allFirst)
	}
	allSecond, allSecondResponse := allItemsRequest("/api/v1/feedback/items?limit=1&q=voted&cursor=" + *allFirst.NextCursor)
	if allSecondResponse.Code != http.StatusOK || allSecond.HasMore || len(allSecond.Data) != 1 || allSecond.Data[0]["id"] != "fbi_a2" {
		t.Fatalf("second workspace feedback search = %d %+v", allSecondResponse.Code, allSecond)
	}
	available, availableResponse := allItemsRequest("/api/v1/feedback/items?q=voted&conversation_id=conv_feedback_a&link_state=available")
	if availableResponse.Code != http.StatusOK || available.HasMore || len(available.Data) != 1 || available.Data[0]["id"] != "fbi_a2" {
		t.Fatalf("available workspace feedback search = %d %+v", availableResponse.Code, available)
	}
	linked, linkedResponse := allItemsRequest("/api/v1/feedback/items?conversation_id=conv_feedback_a&link_state=linked")
	if linkedResponse.Code != http.StatusOK || linked.HasMore || len(linked.Data) != 1 || linked.Data[0]["id"] != "fbi_a1" {
		t.Fatalf("linked workspace feedback search = %d %+v", linkedResponse.Code, linked)
	}
	_, invalidLinkStateResponse := allItemsRequest("/api/v1/feedback/items?link_state=unknown")
	if invalidLinkStateResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid feedback link state status = %d", invalidLinkStateResponse.Code)
	}
	privateCommentRequest := func(path string) (Page[feedback.Comment], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		req.SetPathValue("id", "fbi_a1")
		response := httptest.NewRecorder()
		handleListFeedbackComments(deps)(response, req)
		var page Page[feedback.Comment]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}
	privateFirst, privateFirstResponse := privateCommentRequest("/api/v1/feedback/items/fbi_a1/comments?limit=1")
	if privateFirstResponse.Code != http.StatusOK || !privateFirst.HasMore || privateFirst.NextCursor == nil || len(privateFirst.Data) != 1 || privateFirst.Data[0].ID != "comment_a1" {
		t.Fatalf("first private comment page = %d %+v", privateFirstResponse.Code, privateFirst)
	}
	privateSecond, privateSecondResponse := privateCommentRequest("/api/v1/feedback/items/fbi_a1/comments?limit=1&cursor=" + *privateFirst.NextCursor)
	if privateSecondResponse.Code != http.StatusOK || privateSecond.HasMore || len(privateSecond.Data) != 1 || privateSecond.Data[0].ID != "comment_a2" {
		t.Fatalf("second private comment page = %d %+v", privateSecondResponse.Code, privateSecond)
	}

	publicRequest := func(path string) (Page[feedback.Item], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(ctx)
		req.SetPathValue("workspaceID", "wrk_feedback_a")
		req.SetPathValue("slug", "a-board")
		response := httptest.NewRecorder()
		handlePublicFeedbackItems(deps)(response, req)
		var page Page[feedback.Item]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}
	publicFirst, publicFirstResponse := publicRequest("/api/v1/public/feedback/wrk_feedback_a/boards/a-board/items?limit=1&sort=votes")
	if publicFirstResponse.Code != http.StatusOK || !publicFirst.HasMore || publicFirst.NextCursor == nil || len(publicFirst.Data) != 1 || publicFirst.Data[0].ID != "fbi_a1" {
		t.Fatalf("first public feedback page = %d %+v", publicFirstResponse.Code, publicFirst)
	}
	publicSecond, publicSecondResponse := publicRequest("/api/v1/public/feedback/wrk_feedback_a/boards/a-board/items?limit=1&sort=votes&cursor=" + *publicFirst.NextCursor)
	if publicSecondResponse.Code != http.StatusOK || publicSecond.HasMore || len(publicSecond.Data) != 1 || publicSecond.Data[0].ID != "fbi_a2" {
		t.Fatalf("second public feedback page = %d %+v", publicSecondResponse.Code, publicSecond)
	}

	commentRequest := func(path string) (Page[feedback.Comment], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		req.SetPathValue("workspaceID", "wrk_feedback_a")
		req.SetPathValue("id", "fbi_a1")
		response := httptest.NewRecorder()
		handlePublicListFeedbackComments(deps)(response, req)
		var page Page[feedback.Comment]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}
	commentsFirst, commentsFirstResponse := commentRequest("/api/v1/public/feedback/wrk_feedback_a/items/fbi_a1/comments?limit=1")
	if commentsFirstResponse.Code != http.StatusOK || !commentsFirst.HasMore || commentsFirst.NextCursor == nil || len(commentsFirst.Data) != 1 || commentsFirst.Data[0].ID != "comment_a1" {
		t.Fatalf("first public comment page = %d %+v", commentsFirstResponse.Code, commentsFirst)
	}
	commentsSecond, commentsSecondResponse := commentRequest("/api/v1/public/feedback/wrk_feedback_a/items/fbi_a1/comments?limit=1&cursor=" + *commentsFirst.NextCursor)
	if commentsSecondResponse.Code != http.StatusOK || commentsSecond.HasMore || len(commentsSecond.Data) != 1 || commentsSecond.Data[0].ID != "comment_a2" {
		t.Fatalf("second public comment page = %d %+v", commentsSecondResponse.Code, commentsSecond)
	}
}
