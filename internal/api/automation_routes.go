package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/automation"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerAutomationRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/automation/rules", requireCapability(deps, authorization.AutomationManage, handleListAutomationRules(deps)))
	mux.HandleFunc("POST /v1/automation/rules", requireCapability(deps, authorization.AutomationManage, Idempotency(deps)(handleCreateAutomationRule(deps))))
	mux.HandleFunc("GET /v1/automation/rules/{id}", requireCapability(deps, authorization.AutomationManage, handleGetAutomationRule(deps)))
	mux.HandleFunc("PATCH /v1/automation/rules/{id}", requireCapability(deps, authorization.AutomationManage, idempotent(handleUpdateAutomationRule(deps))))
	mux.HandleFunc("POST /v1/automation/rules/{id}/dry-run", requireCapability(deps, authorization.AutomationManage, idempotent(handleDryRunAutomationRule(deps))))
	mux.HandleFunc("GET /v1/automation/executions", requireCapability(deps, authorization.AutomationManage, handleListAutomationExecutions(deps)))
	mux.HandleFunc("GET /v1/automation/macros", requireCapability(deps, authorization.AutomationManage, handleListMacros(deps)))
	mux.HandleFunc("POST /v1/automation/macros", requireCapability(deps, authorization.AutomationManage, Idempotency(deps)(handleCreateMacro(deps))))
	mux.HandleFunc("POST /v1/automation/macros/{id}/use", requireCapability(deps, authorization.AutomationManage, idempotent(handleUseMacro(deps))))
	mux.HandleFunc("GET /v1/automation/replies", requireCapability(deps, authorization.AutomationManage, handleListSavedReplies(deps)))
	mux.HandleFunc("POST /v1/automation/replies", requireCapability(deps, authorization.AutomationManage, Idempotency(deps)(handleCreateSavedReply(deps))))
	mux.HandleFunc("POST /v1/automation/replies/{id}/use", requireCapability(deps, authorization.AutomationManage, idempotent(handleUseSavedReply(deps))))
}
func handleListAutomationRules(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed automation cursor.")
			return
		}
		var beforePosition *int
		if !cursor.IsZero() {
			position, parseErr := strconv.Atoi(cursor.Value)
			if parseErr != nil || cursor.At.IsZero() {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed automation cursor.")
				return
			}
			beforePosition = &position
		}
		items, err := deps.Automation.ListPage(r.Context(), actorFromRequest(r).WorkspaceID, beforePosition, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeAutomationInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item automation.Rule) Cursor {
			return Cursor{Value: strconv.Itoa(item.Position), At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
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
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		item, err := deps.Automation.ListExecutionsPage(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("rule_id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeAutomationInternal(w, r)
			return
		}
		page := NewPage(item, limit, func(execution automation.Execution) Cursor {
			return Cursor{At: execution.OccurredAt, ID: execution.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleListMacros(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.Automation.ListMacrosPage(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("q"), cursor.Value, cursor.ID, limit+1)
		if err != nil {
			writeAutomationInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(items, limit, func(item automation.Macro) Cursor {
			return Cursor{Value: item.Name, ID: item.ID}
		}))
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
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.Automation.ListSavedRepliesPage(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("q"), cursor.Value, cursor.ID, limit+1)
		if err != nil {
			writeAutomationInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(items, limit, func(item automation.SavedReply) Cursor {
			return Cursor{Value: item.Name, ID: item.ID}
		}))
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
