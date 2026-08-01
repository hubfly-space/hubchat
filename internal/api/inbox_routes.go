package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/inbox"
)

// registerInboxRoutes mounts inbox configuration: creation, channel and team
// assignment, and deletion. Listing inboxes for the nav sidebar is served by
// the bootstrap payload; this is the settings screen's CRUD surface.
func registerInboxRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/inboxes",
		requireActor(deps, handleListInboxes(deps)))
	mux.HandleFunc("POST /v1/inboxes",
		requireCapability(deps, authorization.WorkspaceManage, idempotent(handleCreateInbox(deps))))
	mux.HandleFunc("GET /v1/inboxes/{id}",
		requireActor(deps, handleGetInbox(deps)))
	mux.HandleFunc("PATCH /v1/inboxes/{id}",
		requireCapability(deps, authorization.WorkspaceManage, idempotent(handleUpdateInbox(deps))))
	mux.HandleFunc("PUT /v1/inboxes/{id}/default",
		requireCapability(deps, authorization.WorkspaceManage, idempotent(handleSetDefaultInbox(deps))))
	mux.HandleFunc("DELETE /v1/inboxes/{id}",
		requireCapability(deps, authorization.WorkspaceManage, idempotent(handleDeleteInbox(deps))))
}

type inboxDetailJSON struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description *string  `json:"description"`
	Channels    []string `json:"channels"`
	TeamIDs     []string `json:"team_ids"`
	IsDefault   bool     `json:"is_default"`
	SLAPolicyID *string  `json:"sla_policy_id"`
	OpenCount   int      `json:"open_count"`
	CreatedAt   string   `json:"created_at"`
}

func inboxDetailToJSON(i inbox.Inbox) inboxDetailJSON {
	return inboxDetailJSON{
		ID: i.ID, WorkspaceID: i.WorkspaceID, Name: i.Name, Slug: i.Slug,
		Description: i.Description, Channels: orEmpty(i.Channels), TeamIDs: orEmpty(i.TeamIDs),
		IsDefault: i.IsDefault, SLAPolicyID: i.SLAPolicyID, OpenCount: i.OpenCount,
		CreatedAt: i.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func handleListInboxes(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed inbox cursor.")
			return
		}
		beforeDefault := false
		beforeName := ""
		hasCursor := !cursor.IsZero()
		if hasCursor {
			defaultText, name, ok := strings.Cut(cursor.Value, "|")
			if !ok || name == "" {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed inbox cursor.")
				return
			}
			beforeDefault, err = strconv.ParseBool(defaultText)
			if err != nil || cursor.ID == "" {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed inbox cursor.")
				return
			}
			beforeName = name
		}
		inboxes, err := deps.Inbox.ListPage(r.Context(), actor.WorkspaceID, beforeDefault, beforeName, cursor.ID, hasCursor, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load inboxes.")
			return
		}

		page := NewPage(inboxes, limit, func(item inbox.Inbox) Cursor {
			return Cursor{Value: strconv.FormatBool(item.IsDefault) + "|" + item.Name, ID: item.ID}
		})
		pageOut := make([]inboxDetailJSON, 0, len(page.Data))
		for _, item := range page.Data {
			pageOut = append(pageOut, inboxDetailToJSON(item))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[inboxDetailJSON]{Data: pageOut, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleGetInbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		i, err := deps.Inbox.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeInboxError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, inboxDetailToJSON(*i))
	}
}

type inboxPayload struct {
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description *string  `json:"description"`
	Channels    []string `json:"channels"`
	TeamIDs     []string `json:"team_ids"`
}

func handleCreateInbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req inboxPayload
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		i, err := deps.Inbox.Create(
			r.Context(), actor.WorkspaceID, actor.MemberID,
			req.Name, req.Slug, req.Description, req.Channels, req.TeamIDs,
		)
		if err != nil {
			writeInboxError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, inboxDetailToJSON(*i))
	}
}

func handleUpdateInbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req inboxPayload
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		i, err := deps.Inbox.Update(
			r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"),
			req.Name, req.Description, req.Channels, req.TeamIDs,
		)
		if err != nil {
			writeInboxError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, inboxDetailToJSON(*i))
	}
}

func handleSetDefaultInbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Inbox.SetDefault(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeInboxError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeleteInbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Inbox.Delete(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeInboxError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeInboxError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, inbox.ErrInvalidName), errors.Is(err, inbox.ErrInvalidSlug), errors.Is(err, inbox.ErrInvalidChannel):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, inbox.ErrSlugTaken):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, inbox.ErrLastInbox), errors.Is(err, inbox.ErrHasConversations):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, inbox.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "No such inbox.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
