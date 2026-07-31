package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/notification"
)

func registerNotificationRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/notifications", requireActor(deps, handleListNotifications(deps)))
	mux.HandleFunc("GET /v1/notifications/count", requireActor(deps, handleNotificationCount(deps)))
	mux.HandleFunc("GET /v1/notifications/preferences", requireActor(deps, handleGetNotificationPreferences(deps)))
	mux.HandleFunc("PUT /v1/notifications/preferences", requireActor(deps, Idempotency(deps)(handleSaveNotificationPreferences(deps))))
	mux.HandleFunc("POST /v1/notifications/{id}/read", requireActor(deps, handleMarkNotificationRead(deps)))
	mux.HandleFunc("POST /v1/notifications/read-all", requireActor(deps, handleMarkAllNotificationsRead(deps)))
}

func handleGetNotificationPreferences(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		items, err := deps.Notification.Preferences(r.Context(), actor.WorkspaceID, actor.MemberID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load notification preferences.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}

func handleSaveNotificationPreferences(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var request struct {
			Data []notification.PreferenceInput `json:"data"`
		}
		if err := httpserver.DecodeJSON(r, &request); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed notification preferences.")
			return
		}
		items, err := deps.Notification.SavePreferences(r.Context(), actor.WorkspaceID, actor.MemberID, request.Data)
		if errors.Is(err, notification.ErrInvalidPreference) {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "One or more notification preference types is invalid or duplicated.")
			return
		}
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not save notification preferences.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}

func handleListNotifications(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}

		unread := false
		if raw := r.URL.Query().Get("unread"); raw != "" {
			unread, err = strconv.ParseBool(raw)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "unread must be true or false.")
				return
			}
		}

		items, err := deps.Notification.List(r.Context(), actor.WorkspaceID, actor.MemberID, notification.ListFilter{
			Before: cursor.At, BeforeID: cursor.ID, Limit: limit + 1, Unread: unread,
		})
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load notifications.")
			return
		}
		page := NewPage(items, limit, func(item notification.Notification) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleNotificationCount(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		count, err := deps.Notification.UnreadCount(r.Context(), actor.WorkspaceID, actor.MemberID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load notification count.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]int{"count": count})
	}
}

func handleMarkNotificationRead(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Notification.MarkRead(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			if errors.Is(err, notification.ErrNotFound) {
				httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Notification not found.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not mark notification read.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMarkAllNotificationsRead(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Notification.MarkAllRead(r.Context(), actor.WorkspaceID, actor.MemberID); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not mark notifications read.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
