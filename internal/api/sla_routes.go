package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/sla"
)

func registerSLARoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/sla/calendars", requireCapability(deps, authorization.SLAManage, handleListCalendars(deps)))
	mux.HandleFunc("POST /v1/sla/calendars", requireCapability(deps, authorization.SLAManage, Idempotency(deps)(handleCreateCalendar(deps))))
	mux.HandleFunc("GET /v1/sla/calendars/{id}", requireCapability(deps, authorization.SLAManage, handleGetCalendar(deps)))
	mux.HandleFunc("GET /v1/sla/policies", requireCapability(deps, authorization.SLAManage, handleListSLAPolicies(deps)))
	mux.HandleFunc("POST /v1/sla/policies", requireCapability(deps, authorization.SLAManage, Idempotency(deps)(handleCreateSLAPolicy(deps))))
	mux.HandleFunc("GET /v1/sla/policies/{id}", requireCapability(deps, authorization.SLAManage, handleGetSLAPolicy(deps)))
	mux.HandleFunc("PATCH /v1/sla/policies/{id}", requireCapability(deps, authorization.SLAManage, handleUpdateSLAPolicy(deps)))
	mux.HandleFunc("GET /v1/sla/instances", requireCapability(deps, authorization.SLAManage, handleListSLAInstances(deps)))
}

func handleListSLAInstances(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed pagination parameters.")
			return
		}
		items, err := deps.SLA.ListInstances(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("state"), limit)
		if err != nil {
			writeSLAInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleListCalendars(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.SLA.ListCalendars(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeSLAInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleCreateCalendar(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input sla.CalendarInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		item, err := deps.SLA.CreateCalendar(r.Context(), actorFromRequest(r).WorkspaceID, input)
		if err != nil {
			writeSLAError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleGetCalendar(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.SLA.GetCalendar(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeSLAError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleListSLAPolicies(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.SLA.ListPolicies(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeSLAInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleCreateSLAPolicy(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input sla.PolicyInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		item, err := deps.SLA.CreatePolicy(r.Context(), actorFromRequest(r).WorkspaceID, input)
		if err != nil {
			writeSLAError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleGetSLAPolicy(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.SLA.GetPolicy(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeSLAError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleUpdateSLAPolicy(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil || input.Enabled == nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "enabled is required.")
			return
		}
		item, err := deps.SLA.SetPolicyEnabled(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), *input.Enabled)
		if err != nil {
			writeSLAError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func writeSLAError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sla.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "SLA resource not found.")
	case errors.Is(err, sla.ErrInvalidName), errors.Is(err, sla.ErrInvalidTarget), errors.Is(err, sla.ErrInvalidTimezone), errors.Is(err, sla.ErrInvalidWindow):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		writeSLAInternal(w, r)
	}
}
func writeSLAInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load SLA configuration.")
}
