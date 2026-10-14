package api

import (
	"errors"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/push"
)

func registerPushRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/notifications/push/config", requirePushActor(deps, func(w http.ResponseWriter, r *http.Request) {
		enabled := deps.Push != nil && deps.Push.Enabled()
		key := ""
		if enabled {
			key = deps.Push.PublicKey()
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "public_key": key})
	}))
	mux.HandleFunc("POST /v1/notifications/push/subscriptions", requirePushActor(deps, func(w http.ResponseWriter, r *http.Request) {
		if deps.Push == nil {
			httpserver.WriteError(w, r, http.StatusServiceUnavailable, httpserver.CodeBadRequest, "Push delivery is not configured.")
			return
		}
		var input struct {
			webpush.Subscription
			ExpirationTime *float64 `json:"expirationTime"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed push subscription.")
			return
		}
		actor := actorFromRequest(r)
		id, err := deps.Push.Subscribe(r.Context(), actor.WorkspaceID, actor.MemberID, input.Subscription)
		if err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, push.ErrDisabled) {
				status = http.StatusServiceUnavailable
			}
			httpserver.WriteError(w, r, status, httpserver.CodeValidationError, err.Error())
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
	}))
	mux.HandleFunc("DELETE /v1/notifications/push/subscriptions/{id}", requirePushActor(deps, func(w http.ResponseWriter, r *http.Request) {
		if deps.Push == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		actor := actorFromRequest(r)
		if err := deps.Push.UnsubscribeID(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not remove subscription.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("DELETE /v1/notifications/push/subscriptions", requirePushActor(deps, func(w http.ResponseWriter, r *http.Request) {
		if deps.Push == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var input struct {
			Endpoint string `json:"endpoint"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed subscription.")
			return
		}
		actor := actorFromRequest(r)
		if err := deps.Push.Unsubscribe(r.Context(), actor.WorkspaceID, actor.MemberID, input.Endpoint); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not remove subscription.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func requirePushActor(deps Deps, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Subscriptions belong to a browser member, never to an API key's
		// creator. Force the cookie-session boundary, including SSO policy.
		r = r.Clone(r.Context())
		r.Header.Del("Authorization")
		r.Header.Del("X-API-Key")
		r.Header.Del("Hubchat-API-Key")
		requireActor(deps, next)(w, r)
	}
}
