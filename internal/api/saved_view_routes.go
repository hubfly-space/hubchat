package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/savedview"
)

func registerSavedViewRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/saved-views", requireSavedViewActor(deps, handleListSavedViews(deps)))
	mux.HandleFunc("POST /v1/saved-views", requireSavedViewActor(deps, idempotent(handleCreateSavedView(deps))))
	mux.HandleFunc("GET /v1/saved-views/{id}", requireSavedViewActor(deps, handleGetSavedView(deps)))
	mux.HandleFunc("PATCH /v1/saved-views/{id}", requireSavedViewActor(deps, idempotent(handleUpdateSavedView(deps))))
	mux.HandleFunc("DELETE /v1/saved-views/{id}", requireSavedViewActor(deps, idempotent(handleDeleteSavedView(deps))))
}

func requireSavedViewActor(deps Deps, next http.HandlerFunc) http.HandlerFunc {
	return requireActor(deps, func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if r.Method == http.MethodGet && r.PathValue("id") == "" && !canReadSavedView(actor, r.URL.Query().Get("entity_type")) {
			writeSavedViewForbidden(w, r)
			return
		}
		next(w, r)
	})
}

func handleListSavedViews(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed saved-view cursor.")
			return
		}
		var beforePosition *int
		if !cursor.IsZero() {
			position, parseErr := strconv.Atoi(cursor.Value)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed saved-view cursor.")
				return
			}
			beforePosition = &position
		}
		actor := actorFromRequest(r)
		items, err := deps.SavedView.ListPage(r.Context(), actor.WorkspaceID, actor.MemberID, actor.Role, r.URL.Query().Get("entity_type"), beforePosition, cursor.ID, limit+1)
		if err != nil {
			writeSavedViewInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item savedview.View) Cursor {
			return Cursor{Value: strconv.Itoa(item.Position), ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleCreateSavedView(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input savedview.Input
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		if !canWriteSavedView(actor, input.EntityType) {
			writeSavedViewForbidden(w, r)
			return
		}
		item, err := deps.SavedView.Create(r.Context(), actor.WorkspaceID, actor.MemberID, input)
		if err != nil {
			writeSavedViewError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}

func handleGetSavedView(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		item, err := deps.SavedView.Get(r.Context(), actor.WorkspaceID, actor.MemberID, actor.Role, r.PathValue("id"))
		if err != nil {
			writeSavedViewError(w, r, err)
			return
		}
		if !canReadSavedView(actor, item.EntityType) {
			writeSavedViewForbidden(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleUpdateSavedView(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input savedview.Input
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		current, getErr := deps.SavedView.Get(r.Context(), actor.WorkspaceID, actor.MemberID, actor.Role, r.PathValue("id"))
		if getErr != nil {
			writeSavedViewError(w, r, getErr)
			return
		}
		if input.EntityType == "" {
			input.EntityType = current.EntityType
		}
		if !canWriteSavedView(actor, current.EntityType) || !canWriteSavedView(actor, input.EntityType) {
			writeSavedViewForbidden(w, r)
			return
		}
		item, err := deps.SavedView.Update(r.Context(), actor.WorkspaceID, actor.MemberID, actor.Role, r.PathValue("id"), input)
		if err != nil {
			writeSavedViewError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleDeleteSavedView(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		current, getErr := deps.SavedView.Get(r.Context(), actor.WorkspaceID, actor.MemberID, actor.Role, r.PathValue("id"))
		if getErr != nil {
			writeSavedViewError(w, r, getErr)
			return
		}
		if !canWriteSavedView(actor, current.EntityType) {
			writeSavedViewForbidden(w, r)
			return
		}
		if err := deps.SavedView.Delete(r.Context(), actor.WorkspaceID, actor.MemberID, actor.Role, r.PathValue("id")); err != nil {
			writeSavedViewError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func canReadSavedView(actor *authorization.Actor, entityType string) bool {
	switch entityType {
	case "ticket":
		return actor.Can(authorization.TicketManage)
	case "conversation":
		return actor.Can(authorization.ConversationRead)
	case "":
		return actor.Can(authorization.ConversationRead) || actor.Can(authorization.TicketManage)
	default:
		return false
	}
}

func canWriteSavedView(actor *authorization.Actor, entityType string) bool {
	switch entityType {
	case "ticket":
		return actor.Can(authorization.TicketManage)
	case "", "conversation":
		return actor.Can(authorization.ConversationAssign)
	default:
		return false
	}
}

func writeSavedViewForbidden(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "You do not have permission to use this saved view.")
}

func writeSavedViewError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, savedview.ErrNotFound), errors.Is(err, savedview.ErrNotOwner):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Saved view not found.")
	case errors.Is(err, savedview.ErrInvalidName), errors.Is(err, savedview.ErrInvalidEntity), errors.Is(err, savedview.ErrInvalidScope), errors.Is(err, savedview.ErrInvalidTarget), errors.Is(err, savedview.ErrInvalidFilters):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		writeSavedViewInternal(w, r)
	}
}

func writeSavedViewInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load saved views.")
}
