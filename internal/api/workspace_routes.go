package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

func registerWorkspaceRoutes(mux *http.ServeMux, deps Deps) {
	// Bootstrap requires only a session, not an existing workspace membership
	// — this is the endpoint §7.2's onboarding flow calls to create the very
	// first workspace for a new owner.
	mux.HandleFunc("POST /v1/workspaces", handleCreateWorkspace(deps))

	mux.HandleFunc("GET /v1/inboxes",
		requireCapability(deps, authorization.ConversationRead, handleListInboxes(deps)))
}

func handleListInboxes(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		inboxes, err := deps.Workspace.ListInboxes(r.Context(), actor.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load inboxes.")
			return
		}

		out := make([]any, len(inboxes))
		for i, inbox := range inboxes {
			out[i] = map[string]any{
				"id":         inbox.ID,
				"name":       inbox.Name,
				"slug":       inbox.Slug,
				"is_default": inbox.IsDefault,
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type createWorkspaceRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func handleCreateWorkspace(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httpserver.SessionToken(r)
		if token == "" {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Sign in to continue.")
			return
		}

		user, err := deps.Auth.UserForSession(r.Context(), token)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Your session has expired.")
			return
		}

		var req createWorkspaceRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		ws, err := deps.Workspace.Bootstrap(r.Context(), user.ID, req.Name, req.Slug)
		if err != nil {
			switch {
			case errors.Is(err, workspace.ErrInvalidName):
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
			case errors.Is(err, workspace.ErrInvalidSlug):
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
			case errors.Is(err, workspace.ErrSlugTaken):
				httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
			default:
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not create workspace.")
			}
			return
		}

		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{
			"id":   ws.ID,
			"name": ws.Name,
			"slug": ws.Slug,
		})
	}
}
