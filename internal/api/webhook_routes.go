package api

import (
	"errors"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/webhook"
	"net/http"
)

func registerWebhookRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/webhooks", requireCapability(deps, authorization.IntegrationManage, handleListWebhooks(deps)))
	mux.HandleFunc("POST /v1/webhooks", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleCreateWebhook(deps))))
	mux.HandleFunc("GET /v1/webhooks/{id}", requireCapability(deps, authorization.IntegrationManage, handleGetWebhook(deps)))
	mux.HandleFunc("PATCH /v1/webhooks/{id}", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleUpdateWebhook(deps))))
	mux.HandleFunc("DELETE /v1/webhooks/{id}", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleDeleteWebhook(deps))))
	mux.HandleFunc("POST /v1/webhooks/{id}/rotate-secret", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleRotateWebhook(deps))))
	mux.HandleFunc("GET /v1/webhooks/{id}/deliveries", requireCapability(deps, authorization.IntegrationManage, handleListWebhookDeliveries(deps)))
	mux.HandleFunc("POST /v1/webhooks/{id}/deliveries/{deliveryID}/replay", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleReplayWebhook(deps))))
	mux.HandleFunc("POST /v1/webhooks/{id}/test", requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleTestWebhook(deps))))
}

type webhookRequest struct {
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Events      []string `json:"events"`
	Enabled     *bool    `json:"enabled"`
}

func handleListWebhooks(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoints, err := deps.Webhook.List(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeWebhookInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": endpoints})
	}
}

func handleCreateWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req webhookRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if req.Enabled == nil {
			value := true
			req.Enabled = &value
		}
		actor := actorFromRequest(r)
		created, err := deps.Webhook.Create(r.Context(), actor.WorkspaceID, actor.MemberID, webhook.Input{URL: req.URL, Description: req.Description, Events: req.Events, Enabled: *req.Enabled})
		if err != nil {
			writeWebhookError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"data": created.Endpoint, "secret": created.Secret})
	}
}

func handleGetWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoint, err := deps.Webhook.Get(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeWebhookError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, endpoint)
	}
}

func handleUpdateWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req webhookRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if req.Enabled == nil {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "enabled is required.")
			return
		}
		endpoint, err := deps.Webhook.Update(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), webhook.Input{URL: req.URL, Description: req.Description, Events: req.Events, Enabled: *req.Enabled})
		if err != nil {
			writeWebhookError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, endpoint)
	}
}

func handleDeleteWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := deps.Webhook.Delete(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id")); err != nil {
			writeWebhookError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRotateWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		created, err := deps.Webhook.RotateSecret(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeWebhookError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"data": created.Endpoint, "secret": created.Secret})
	}
}

func handleListWebhookDeliveries(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		actor := actorFromRequest(r)
		endpointID := r.PathValue("id")
		if _, err := deps.Webhook.Get(r.Context(), actor.WorkspaceID, endpointID); err != nil {
			writeWebhookError(w, r, err)
			return
		}
		deliveries, err := deps.Webhook.Deliveries(r.Context(), actor.WorkspaceID, endpointID, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeWebhookInternal(w, r)
			return
		}
		page := NewPage(deliveries, limit, func(d webhook.Delivery) Cursor { return Cursor{At: d.CreatedAt, ID: d.ID} })
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleReplayWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		delivery, err := deps.Webhook.Replay(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.PathValue("deliveryID"))
		if err != nil {
			writeWebhookError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusAccepted, delivery)
	}
}

func handleTestWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		delivery, err := deps.Webhook.Test(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeWebhookError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusAccepted, delivery)
	}
}

func writeWebhookError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, webhook.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Webhook endpoint not found.")
	case errors.Is(err, webhook.ErrInvalidURL), errors.Is(err, webhook.ErrInvalidEvents):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		writeWebhookInternal(w, r)
	}
}
func writeWebhookInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not update webhooks.")
}
