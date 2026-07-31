package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerAnalyticsRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/analytics/rollups", requireCapability(deps, authorization.ReportRead, handleListAnalyticsRollups(deps)))
	mux.HandleFunc("GET /v1/reports", requireCapability(deps, authorization.ReportRead, handleListReports(deps)))
	mux.HandleFunc("POST /v1/reports", requireCapability(deps, authorization.ReportRead, Idempotency(deps)(handleCreateReport(deps))))
	mux.HandleFunc("GET /v1/reports/{id}", requireCapability(deps, authorization.ReportRead, handleGetReport(deps)))
}
func handleListAnalyticsRollups(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var from, to time.Time
		if value := r.URL.Query().Get("from"); value != "" {
			from, _ = time.Parse(time.RFC3339, value)
		}
		if value := r.URL.Query().Get("to"); value != "" {
			to, _ = time.Parse(time.RFC3339, value)
		}
		items, err := deps.Analytics.Rollups(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("metric"), r.URL.Query().Get("grain"), from, to)
		if err != nil {
			writeAnalyticsInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleListReports(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.Analytics.ListReports(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeAnalyticsInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}
func handleCreateReport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input analytics.ReportInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Analytics.CreateReport(r.Context(), actor.WorkspaceID, actor.MemberID, input)
		if err != nil {
			if errors.Is(err, analytics.ErrInvalidReportName) {
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
				return
			}
			writeAnalyticsInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleGetReport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Analytics.GetReport(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeAnalyticsInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func writeAnalyticsInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load analytics.")
}
