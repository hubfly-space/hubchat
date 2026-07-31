package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/automation"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerAutomationRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/automation/rules", requireCapability(deps, authorization.AutomationManage, handleListAutomationRules(deps)))
	mux.HandleFunc("POST /v1/automation/rules", requireCapability(deps, authorization.AutomationManage, Idempotency(deps)(handleCreateAutomationRule(deps))))
	mux.HandleFunc("GET /v1/automation/rules/{id}", requireCapability(deps, authorization.AutomationManage, handleGetAutomationRule(deps)))
	mux.HandleFunc("PATCH /v1/automation/rules/{id}", requireCapability(deps, authorization.AutomationManage, handleUpdateAutomationRule(deps)))
	mux.HandleFunc("POST /v1/automation/rules/{id}/dry-run", requireCapability(deps, authorization.AutomationManage, handleDryRunAutomationRule(deps)))
	mux.HandleFunc("GET /v1/automation/executions", requireCapability(deps, authorization.AutomationManage, handleListAutomationExecutions(deps)))
	mux.HandleFunc("GET /v1/automation/macros", requireCapability(deps, authorization.AutomationManage, handleListMacros(deps)))
	mux.HandleFunc("POST /v1/automation/macros", requireCapability(deps, authorization.AutomationManage, Idempotency(deps)(handleCreateMacro(deps))))
	mux.HandleFunc("POST /v1/automation/macros/{id}/use", requireCapability(deps, authorization.AutomationManage, handleUseMacro(deps)))
	mux.HandleFunc("GET /v1/automation/replies", requireCapability(deps, authorization.AutomationManage, handleListSavedReplies(deps)))
	mux.HandleFunc("POST /v1/automation/replies", requireCapability(deps, authorization.AutomationManage, Idempotency(deps)(handleCreateSavedReply(deps))))
	mux.HandleFunc("POST /v1/automation/replies/{id}/use", requireCapability(deps, authorization.AutomationManage, handleUseSavedReply(deps)))
}
func handleListAutomationRules(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.Automation.List(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeAutomationInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleCreateAutomationRule(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input automation.Input
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Automation.Create(r.Context(), actor.WorkspaceID, actor.MemberID, input)
		if err != nil {
			writeAutomationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleGetAutomationRule(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Automation.Get(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeAutomationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleUpdateAutomationRule(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input automation.Input
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Automation.Update(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), input)
		if err != nil {
			writeAutomationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleDryRunAutomationRule(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			EventID     string `json:"event_id"`
			SubjectType string `json:"subject_type"`
			SubjectID   string `json:"subject_id"`
			Depth       int    `json:"depth"`
			CausationID string `json:"causation_id"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Automation.Execute(r.Context(), actor.WorkspaceID, r.PathValue("id"), input.EventID, input.SubjectType, input.SubjectID, input.CausationID, input.Depth, true)
		if err != nil && !errors.Is(err, automation.ErrDepthExceeded) {
			writeAutomationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleListAutomationExecutions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Automation.ListExecutions(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("rule_id"), 100)
		if err != nil {
			writeAutomationInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": item})
	}
}

func handleListMacros(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.Automation.ListMacros(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("q"))
		if err != nil {
			writeAutomationInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleCreateMacro(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input automation.MacroInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Automation.CreateMacro(r.Context(), actor.WorkspaceID, actor.MemberID, input)
		if err != nil {
			writeAutomationContentError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleUseMacro(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Automation.UseMacro(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			writeAutomationContentError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
func handleListSavedReplies(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.Automation.ListSavedReplies(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("q"))
		if err != nil {
			writeAutomationInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleCreateSavedReply(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input automation.SavedReplyInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Automation.CreateSavedReply(r.Context(), actor.WorkspaceID, actor.MemberID, input)
		if err != nil {
			writeAutomationContentError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleUseSavedReply(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Automation.UseSavedReply(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			writeAutomationContentError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
func writeAutomationContentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, automation.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Automation content not found.")
	case errors.Is(err, automation.ErrInvalidName), errors.Is(err, automation.ErrInvalidScope), errors.Is(err, automation.ErrInvalidTarget), errors.Is(err, automation.ErrInvalidShortcut), errors.Is(err, automation.ErrInvalidAction):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		writeAutomationInternal(w, r)
	}
}
func writeAutomationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, automation.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Automation rule not found.")
	case errors.Is(err, automation.ErrInvalidName), errors.Is(err, automation.ErrInvalidTrigger), errors.Is(err, automation.ErrInvalidAction):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, automation.ErrRateLimited), errors.Is(err, automation.ErrDepthExceeded):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	default:
		writeAutomationInternal(w, r)
	}
}
func writeAutomationInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load automation.")
}
