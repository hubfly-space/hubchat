package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/feedback"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerFeedbackRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/feedback/boards", requireCapability(deps, authorization.FeedbackModerate, handleListFeedbackBoards(deps)))
	mux.HandleFunc("POST /v1/feedback/boards", requireCapability(deps, authorization.FeedbackModerate, Idempotency(deps)(handleCreateFeedbackBoard(deps))))
	mux.HandleFunc("GET /v1/feedback/boards/{id}", requireCapability(deps, authorization.FeedbackModerate, handleGetFeedbackBoard(deps)))
	mux.HandleFunc("GET /v1/feedback/boards/{id}/items", requireCapability(deps, authorization.FeedbackModerate, handleListFeedbackItems(deps)))
	mux.HandleFunc("POST /v1/feedback/boards/{id}/items", requireCapability(deps, authorization.FeedbackModerate, Idempotency(deps)(handleCreateFeedbackItem(deps))))
	mux.HandleFunc("GET /v1/feedback/items", requireCapability(deps, authorization.FeedbackModerate, handleListAllFeedbackItems(deps)))
	mux.HandleFunc("GET /v1/feedback/items/{id}", requireCapability(deps, authorization.FeedbackModerate, handleGetFeedbackItem(deps)))
	mux.HandleFunc("GET /v1/feedback/roadmap", requireCapability(deps, authorization.FeedbackModerate, handleListFeedbackRoadmap(deps)))
	mux.HandleFunc("GET /v1/feedback/items/{id}/comments", requireCapability(deps, authorization.FeedbackModerate, handleListFeedbackComments(deps)))
	mux.HandleFunc("GET /v1/feedback/items/{id}/links", requireCapability(deps, authorization.FeedbackModerate, handleListFeedbackLinks(deps)))
	mux.HandleFunc("POST /v1/feedback/items/{id}/links", requireCapability(deps, authorization.FeedbackModerate, Idempotency(deps)(handleAddFeedbackLink(deps))))
	mux.HandleFunc("DELETE /v1/feedback/items/{id}/links/{linkID}", requireCapability(deps, authorization.FeedbackModerate, idempotent(handleRemoveFeedbackLink(deps))))
	mux.HandleFunc("POST /v1/feedback/items/{id}/merge", requireCapability(deps, authorization.FeedbackModerate, Idempotency(deps)(handleMergeFeedbackItem(deps))))
	mux.HandleFunc("PATCH /v1/feedback/items/{id}/status", requireCapability(deps, authorization.FeedbackModerate, idempotent(handleSetFeedbackStatus(deps))))
	mux.HandleFunc("POST /v1/feedback/items/{id}/votes", requireCapability(deps, authorization.FeedbackModerate, idempotent(handleVoteFeedbackItem(deps))))
	mux.HandleFunc("POST /v1/feedback/items/{id}/comments", requireCapability(deps, authorization.FeedbackModerate, idempotent(handleAddFeedbackComment(deps))))

	mux.HandleFunc("GET /v1/public/feedback/{workspaceID}/boards", handlePublicFeedbackBoards(deps))
	mux.HandleFunc("GET /v1/public/feedback/{workspaceID}/boards/{slug}/items", handlePublicFeedbackItems(deps))
	mux.HandleFunc("POST /v1/public/feedback/{workspaceID}/boards/{slug}/items", Idempotency(deps)(handlePublicCreateFeedbackItem(deps)))
	mux.HandleFunc("GET /v1/public/feedback/{workspaceID}/items/{id}", handlePublicGetFeedbackItem(deps))
	mux.HandleFunc("GET /v1/public/feedback/{workspaceID}/items/{id}/comments", handlePublicListFeedbackComments(deps))
	mux.HandleFunc("POST /v1/public/feedback/{workspaceID}/items/{id}/comments", Idempotency(deps)(handlePublicAddFeedbackComment(deps)))
	mux.HandleFunc("POST /v1/public/feedback/{workspaceID}/items/{id}/votes", Idempotency(deps)(handlePublicVoteFeedbackItem(deps)))
	mux.HandleFunc("POST /v1/public/feedback/{workspaceID}/items/{id}/subscription", Idempotency(deps)(handlePublicSubscribeFeedbackItem(deps)))
	mux.HandleFunc("DELETE /v1/public/feedback/{workspaceID}/items/{id}/subscription", Idempotency(deps)(handlePublicUnsubscribeFeedbackItem(deps)))
}

func handleListAllFeedbackItems(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
			return
		}
		linkState := r.URL.Query().Get("link_state")
		if linkState != "" && linkState != "available" && linkState != "linked" {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "link_state must be empty, available, or linked.")
			return
		}
		conversationID := r.URL.Query().Get("conversation_id")
		if linkState != "" && conversationID == "" {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "conversation_id is required when link_state is set.")
			return
		}
		items, err := deps.Feedback.ListItemsPageAll(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("status"), r.URL.Query().Get("q"), conversationID, linkState, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item feedback.Item) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleListFeedbackBoards(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
			return
		}
		var beforePosition *int
		if !cursor.IsZero() {
			position, parseErr := strconv.Atoi(cursor.Value)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
				return
			}
			beforePosition = &position
		}
		items, err := deps.Feedback.ListBoardsPage(r.Context(), actorFromRequest(r).WorkspaceID, beforePosition, cursor.ID, limit+1)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item feedback.Board) Cursor {
			return Cursor{Value: strconv.Itoa(item.Position), ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}
func handleCreateFeedbackBoard(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input feedback.BoardInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFeedbackValidation(w, r, err)
			return
		}
		item, err := deps.Feedback.CreateBoard(r.Context(), actorFromRequest(r).WorkspaceID, input)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleGetFeedbackBoard(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Feedback.GetBoard(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), false)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleListFeedbackItems(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed pagination parameters.")
			return
		}
		actor := actorFromRequest(r)
		var beforeVote *int64
		if r.URL.Query().Get("sort") != "recent" && !cursor.IsZero() {
			value, parseErr := strconv.ParseInt(cursor.Value, 10, 64)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
				return
			}
			beforeVote = &value
		}
		items, err := deps.Feedback.ListItemsPage(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.URL.Query().Get("status"), r.URL.Query().Get("sort"), r.URL.Query().Get("q"), "", cursor.At, cursor.ID, beforeVote, limit+1)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item feedback.Item) Cursor {
			value := ""
			if r.URL.Query().Get("sort") != "recent" {
				value = strconv.Itoa(item.VoteCount)
			}
			return Cursor{Value: value, At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}
func handleCreateFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input feedback.ItemInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFeedbackValidation(w, r, err)
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Feedback.CreateItem(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID, input, "")
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleGetFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		item, err := deps.Feedback.GetItem(r.Context(), actor.WorkspaceID, r.PathValue("id"), "")
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleListFeedbackRoadmap(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
			return
		}
		beforeRank, beforeVote := 0, int64(0)
		if cursor.Value != "" {
			parts := strings.Split(cursor.Value, ":")
			if len(parts) != 2 {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
				return
			}
			beforeRank, err = strconv.Atoi(parts[0])
			if err == nil {
				beforeVote, err = strconv.ParseInt(parts[1], 10, 64)
			}
			if err != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
				return
			}
		}
		status := r.URL.Query().Get("status")
		items, err := deps.Feedback.ListRoadmapItemsPage(r.Context(), actorFromRequest(r).WorkspaceID, status, beforeRank, beforeVote, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item feedback.Item) Cursor {
			return Cursor{Value: strconv.Itoa(roadmapStatusRank(item.Status)) + ":" + strconv.FormatInt(int64(item.VoteCount), 10), At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func roadmapStatusRank(status string) int {
	switch status {
	case "in_progress":
		return 1
	case "planned":
		return 2
	case "completed":
		return 3
	default:
		return 4
	}
}
func handleListFeedbackComments(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
			return
		}
		actor := actorFromRequest(r)
		comments, err := deps.Feedback.ListCommentsPage(r.Context(), actor.WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		page := NewPage(comments, limit, func(item feedback.Comment) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}
func handleListFeedbackLinks(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback link cursor.")
			return
		}
		links, err := deps.Feedback.ListLinksPage(r.Context(), actor.WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(links, limit, func(link feedback.Link) Cursor {
			return Cursor{At: link.CreatedAt, ID: link.ID}
		}))
	}
}
func handleAddFeedbackLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input feedback.LinkInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFeedbackValidation(w, r, err)
			return
		}
		actor := actorFromRequest(r)
		link, err := deps.Feedback.AddLink(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID, input)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, link)
	}
}
func handleRemoveFeedbackLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Feedback.RemoveLink(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.PathValue("linkID")); err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
func handleMergeFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			TargetID string `json:"target_id"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil || input.TargetID == "" {
			writeFeedbackValidation(w, r, errors.New("target_id is required"))
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Feedback.MergeItems(r.Context(), actor.WorkspaceID, r.PathValue("id"), input.TargetID, actor.MemberID)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleSetFeedbackStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Status string `json:"status"`
			Note   string `json:"note"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFeedbackValidation(w, r, err)
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Feedback.SetStatus(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID, input.Status, input.Note)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleVoteFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Feedback.Vote(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID); err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"status": "voted"})
	}
}
func handleAddFeedbackComment(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body     string `json:"body"`
			Official bool   `json:"is_official_update"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFeedbackValidation(w, r, err)
			return
		}
		actor := actorFromRequest(r)
		comment, err := deps.Feedback.AddComment(r.Context(), actor.WorkspaceID, r.PathValue("id"), "agent", actor.MemberID, actor.MemberID, input.Body, input.Official)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, comment)
	}
}

func handlePublicFeedbackBoards(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
			return
		}
		var beforePosition *int
		if !cursor.IsZero() {
			position, parseErr := strconv.Atoi(cursor.Value)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
				return
			}
			beforePosition = &position
		}
		items, err := deps.Feedback.ListPublicBoardsPage(r.Context(), r.PathValue("workspaceID"), beforePosition, cursor.ID, limit+1)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item feedback.Board) Cursor {
			return Cursor{Value: strconv.Itoa(item.Position), ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}
func handlePublicFeedbackItems(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		board, err := deps.Feedback.GetBoard(r.Context(), r.PathValue("workspaceID"), r.PathValue("slug"), true)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		limit, cursor, pageErr := PageParams(r)
		if pageErr != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
			return
		}
		customerID := portalCustomerForRequest(r, deps, r.PathValue("workspaceID"))
		sortOrder := r.URL.Query().Get("sort")
		var beforeVote *int64
		if sortOrder != "recent" && !cursor.IsZero() {
			value, parseErr := strconv.ParseInt(cursor.Value, 10, 64)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
				return
			}
			beforeVote = &value
		}
		items, err := deps.Feedback.ListItemsPage(r.Context(), r.PathValue("workspaceID"), board.ID, r.URL.Query().Get("status"), sortOrder, r.URL.Query().Get("q"), customerID, cursor.At, cursor.ID, beforeVote, limit+1)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item feedback.Item) Cursor {
			value := ""
			if sortOrder != "recent" {
				value = strconv.Itoa(item.VoteCount)
			}
			return Cursor{Value: value, At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}
func handlePublicCreateFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input feedback.ItemInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFeedbackValidation(w, r, err)
			return
		}
		workspaceID := r.PathValue("workspaceID")
		board, err := deps.Feedback.GetBoard(r.Context(), workspaceID, r.PathValue("slug"), true)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		customerID := portalCustomerForRequest(r, deps, workspaceID)
		item, err := deps.Feedback.CreateItem(r.Context(), workspaceID, board.ID, "", input, customerID)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}

func handlePublicGetFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := r.PathValue("workspaceID")
		item, err := publicFeedbackItem(r, deps, workspaceID)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handlePublicListFeedbackComments(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := r.PathValue("workspaceID")
		if _, err := publicFeedbackItem(r, deps, workspaceID); err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed comment cursor.")
			return
		}
		comments, err := deps.Feedback.ListCommentsPage(r.Context(), workspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(comments, limit, func(comment feedback.Comment) Cursor {
			return Cursor{At: comment.CreatedAt, ID: comment.ID}
		}))
	}
}

func handlePublicAddFeedbackComment(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := r.PathValue("workspaceID")
		if _, err := publicFeedbackItem(r, deps, workspaceID); err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		customerID := portalCustomerForRequest(r, deps, workspaceID)
		if customerID == "" {
			writeFeedbackError(w, r, feedback.ErrCustomerRequired)
			return
		}
		customer, err := deps.Customer.Get(r.Context(), workspaceID, customerID)
		if err != nil {
			writeFeedbackError(w, r, feedback.ErrCustomerRequired)
			return
		}
		var input struct {
			Body string `json:"body"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			writeFeedbackValidation(w, r, err)
			return
		}
		authorName := ""
		if customer.Name != nil {
			authorName = *customer.Name
		}
		comment, err := deps.Feedback.AddComment(r.Context(), workspaceID, r.PathValue("id"), "customer", customerID, authorName, input.Body, false)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, comment)
	}
}

func publicFeedbackItem(r *http.Request, deps Deps, workspaceID string) (*feedback.Item, error) {
	customerID := portalCustomerForRequest(r, deps, workspaceID)
	item, err := deps.Feedback.GetItem(r.Context(), workspaceID, r.PathValue("id"), customerID)
	if err != nil {
		return nil, err
	}
	if item.Visibility != "public" {
		return nil, feedback.ErrNotFound
	}
	if _, err := deps.Feedback.GetBoard(r.Context(), workspaceID, item.BoardID, true); err != nil {
		return nil, err
	}
	return item, nil
}
func handlePublicVoteFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := portalCustomerForRequest(r, deps, r.PathValue("workspaceID"))
		if err := deps.Feedback.Vote(r.Context(), r.PathValue("workspaceID"), r.PathValue("id"), customerID); err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"status": "voted"})
	}
}

func handlePublicSubscribeFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := r.PathValue("workspaceID")
		customerID := portalCustomerForRequest(r, deps, workspaceID)
		if err := deps.Feedback.Subscribe(r.Context(), workspaceID, r.PathValue("id"), customerID); err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"subscribed": true})
	}
}

func handlePublicUnsubscribeFeedbackItem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := r.PathValue("workspaceID")
		customerID := portalCustomerForRequest(r, deps, workspaceID)
		if err := deps.Feedback.Unsubscribe(r.Context(), workspaceID, r.PathValue("id"), customerID); err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"subscribed": false})
	}
}

func portalCustomerForRequest(r *http.Request, deps Deps, workspaceID string) string {
	if deps.Portal == nil {
		return ""
	}
	token := httpserver.PortalSessionToken(r)
	if token == "" {
		return ""
	}
	session, err := deps.Portal.Session(r.Context(), token, portalIdentifier(r))
	if err != nil || session.WorkspaceID != workspaceID {
		return ""
	}
	return session.CustomerID
}
func writeFeedbackError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, feedback.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Feedback resource not found.")
	case errors.Is(err, feedback.ErrInvalidName), errors.Is(err, feedback.ErrInvalidSlug), errors.Is(err, feedback.ErrInvalidStatus), errors.Is(err, feedback.ErrInvalidType), errors.Is(err, feedback.ErrInvalidComment), errors.Is(err, feedback.ErrInvalidLink), errors.Is(err, feedback.ErrInvalidMerge):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, feedback.ErrAlreadyVoted), errors.Is(err, feedback.ErrVoteLimit):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, feedback.ErrCustomerRequired):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Sign in to follow feedback updates.")
	case errors.Is(err, feedback.ErrVotingDisabled), errors.Is(err, feedback.ErrCommentsDisabled):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, err.Error())
	default:
		writeFeedbackInternal(w, r)
	}
}
func writeFeedbackValidation(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
}
func writeFeedbackInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load feedback.")
}
