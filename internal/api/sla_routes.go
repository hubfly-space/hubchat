package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/sla"
)

func registerSLARoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/sla/calendars", requireCapability(deps, authorization.SLAManage, handleListCalendars(deps)))
	mux.HandleFunc("POST /v1/sla/calendars", requireCapability(deps, authorization.SLAManage, Idempotency(deps)(handleCreateCalendar(deps))))
	mux.HandleFunc("GET /v1/sla/calendars/{id}", requireCapability(deps, authorization.SLAManage, handleGetCalendar(deps)))
	mux.HandleFunc("PATCH /v1/sla/calendars/{id}", requireCapability(deps, authorization.SLAManage, Idempotency(deps)(handleUpdateCalendar(deps))))
	mux.HandleFunc("GET /v1/sla/policies", requireCapability(deps, authorization.SLAManage, handleListSLAPolicies(deps)))
	mux.HandleFunc("POST /v1/sla/policies", requireCapability(deps, authorization.SLAManage, Idempotency(deps)(handleCreateSLAPolicy(deps))))
	mux.HandleFunc("GET /v1/sla/policies/{id}", requireCapability(deps, authorization.SLAManage, handleGetSLAPolicy(deps)))
	mux.HandleFunc("PATCH /v1/sla/policies/{id}", requireCapability(deps, authorization.SLAManage, Idempotency(deps)(handleUpdateSLAPolicy(deps))))
	mux.HandleFunc("GET /v1/sla/instances", requireCapability(deps, authorization.SLAManage, handleListSLAInstances(deps)))
}

func handleListSLAInstances(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed pagination parameters.")
			return
		}
		items, err := deps.SLA.ListInstances(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("state"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeSLAInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item sla.Instance) Cursor {
			return Cursor{At: item.StartedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func slaJSON(item *sla.SubjectSLA) any {
	if item == nil {
		return nil
	}
	return map[string]any{
		"policy_id":                item.PolicyID,
		"state":                    item.State,
		"first_response_remaining": item.FirstResponseRemaining,
		"next_response_remaining":  item.NextResponseRemaining,
		"resolution_remaining":     item.ResolutionRemaining,
	}
}

func handleListCalendars(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed pagination parameters.")
			return
		}
		var beforeDefault *bool
		beforeName := ""
		if !cursor.IsZero() {
			parts := strings.SplitN(cursor.Value, "\x00", 2)
			if len(parts) != 2 {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed pagination parameters.")
				return
			}
			value, parseErr := strconv.ParseBool(parts[0])
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed pagination parameters.")
				return
			}
			beforeDefault = &value
			beforeName = parts[1]
		}
		items, err := deps.SLA.ListCalendarsPage(r.Context(), actorFromRequest(r).WorkspaceID, beforeDefault, beforeName, cursor.ID, limit+1)
		if err != nil {
			writeSLAInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(items, limit, func(item sla.CalendarRecord) Cursor {
			return Cursor{Value: strconv.FormatBool(item.IsDefault) + "\x00" + item.Name, ID: item.ID}
		}))
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
func handleUpdateCalendar(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input sla.CalendarInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		item, err := deps.SLA.UpdateCalendar(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), input)
		if err != nil {
			writeSLAError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleListSLAPolicies(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed pagination parameters.")
			return
		}
		items, err := deps.SLA.ListPoliciesPage(r.Context(), actorFromRequest(r).WorkspaceID, cursor.Value, cursor.ID, limit+1)
		if err != nil {
			writeSLAInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(items, limit, func(item sla.Policy) Cursor {
			return Cursor{Value: item.Name, ID: item.ID}
		}))
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
		var raw map[string]json.RawMessage
		if err := httpserver.DecodeJSON(r, &raw); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed SLA policy update.")
			return
		}
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		encoded, _ := json.Marshal(raw)
		if err := json.Unmarshal(encoded, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed SLA policy update.")
			return
		}
		if len(raw) == 1 && input.Enabled != nil {
			item, err := deps.SLA.SetPolicyEnabled(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), *input.Enabled)
			if err != nil {
				writeSLAError(w, r, err)
				return
			}
			httpserver.WriteJSON(w, http.StatusOK, item)
			return
		}
		var policyInput sla.PolicyInput
		if err := json.Unmarshal(encoded, &policyInput); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed SLA policy configuration.")
			return
		}
		item, err := deps.SLA.UpdatePolicy(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), policyInput)
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
