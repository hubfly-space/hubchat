package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/automation"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerFollowUpRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/conversations/{id}/follow-ups", requireCapability(deps, authorization.ConversationRead, func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		items, err := deps.Automation.FollowUps(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"))
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[automation.FollowUp]{Data: items})
	}))
	mux.HandleFunc("POST /v1/conversations/{id}/follow-ups", requireCapability(deps, authorization.ConversationReply, Idempotency(deps)(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Body         string    `json:"body"`
			ScheduledFor time.Time `json:"scheduled_for"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed follow-up.")
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Automation.ScheduleFollowUp(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), input.Body, input.ScheduledFor)
		if errors.Is(err, automation.ErrInvalidFollowUp) {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
			return
		}
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	})))
	mux.HandleFunc("DELETE /v1/conversations/{id}/follow-ups/{followUpID}", requireCapability(deps, authorization.ConversationReply, func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Automation.CancelFollowUp(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("followUpID")); err != nil {
			httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Follow-up not found or already running.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}
