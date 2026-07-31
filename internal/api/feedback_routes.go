package api

import (
	"errors"
	"net/http"
	"strconv"

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
	mux.HandleFunc("GET /v1/feedback/items/{id}", requireCapability(deps, authorization.FeedbackModerate, handleGetFeedbackItem(deps)))
	mux.HandleFunc("GET /v1/feedback/roadmap", requireCapability(deps, authorization.FeedbackModerate, handleListFeedbackRoadmap(deps)))
	mux.HandleFunc("GET /v1/feedback/items/{id}/comments", requireCapability(deps, authorization.FeedbackModerate, handleListFeedbackComments(deps)))
	mux.HandleFunc("PATCH /v1/feedback/items/{id}/status", requireCapability(deps, authorization.FeedbackModerate, idempotent(handleSetFeedbackStatus(deps))))
	mux.HandleFunc("POST /v1/feedback/items/{id}/votes", requireCapability(deps, authorization.FeedbackModerate, idempotent(handleVoteFeedbackItem(deps))))
	mux.HandleFunc("POST /v1/feedback/items/{id}/comments", requireCapability(deps, authorization.FeedbackModerate, idempotent(handleAddFeedbackComment(deps))))

	mux.HandleFunc("GET /v1/public/feedback/{workspaceID}/boards", handlePublicFeedbackBoards(deps))
	mux.HandleFunc("GET /v1/public/feedback/{workspaceID}/boards/{slug}/items", handlePublicFeedbackItems(deps))
	mux.HandleFunc("POST /v1/public/feedback/{workspaceID}/boards/{slug}/items", Idempotency(deps)(handlePublicCreateFeedbackItem(deps)))
	mux.HandleFunc("POST /v1/public/feedback/{workspaceID}/items/{id}/votes", Idempotency(deps)(handlePublicVoteFeedbackItem(deps)))
	mux.HandleFunc("POST /v1/public/feedback/{workspaceID}/items/{id}/subscription", Idempotency(deps)(handlePublicSubscribeFeedbackItem(deps)))
	mux.HandleFunc("DELETE /v1/public/feedback/{workspaceID}/items/{id}/subscription", Idempotency(deps)(handlePublicUnsubscribeFeedbackItem(deps)))
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
		items, err := deps.Feedback.ListRoadmapItems(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("status"), 200)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleListFeedbackComments(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		comments, err := deps.Feedback.ListComments(r.Context(), actor.WorkspaceID, r.PathValue("id"), 100)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": comments})
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
		items, err := deps.Feedback.ListBoards(r.Context(), r.PathValue("workspaceID"))
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		public := make([]feedback.Board, 0, len(items))
		for _, item := range items {
			if item.Visibility == "public" {
				public = append(public, item)
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": public})
	}
}
func handlePublicFeedbackItems(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		board, err := deps.Feedback.GetBoard(r.Context(), r.PathValue("workspaceID"), r.PathValue("slug"), true)
		if err != nil {
			writeFeedbackError(w, r, err)
			return
		}
		customerID := portalCustomerForRequest(r, deps, r.PathValue("workspaceID"))
		items, err := deps.Feedback.ListItems(r.Context(), r.PathValue("workspaceID"), board.ID, r.URL.Query().Get("status"), r.URL.Query().Get("sort"), r.URL.Query().Get("q"), customerID, 100)
		if err != nil {
			writeFeedbackInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
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
	case errors.Is(err, feedback.ErrInvalidName), errors.Is(err, feedback.ErrInvalidSlug), errors.Is(err, feedback.ErrInvalidStatus), errors.Is(err, feedback.ErrInvalidType), errors.Is(err, feedback.ErrInvalidComment):
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
