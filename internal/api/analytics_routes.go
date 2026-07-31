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
	mux.HandleFunc("GET /v1/analytics/metrics", requireCapability(deps, authorization.ReportRead, handleListAnalyticsMetrics(deps)))
	mux.HandleFunc("GET /v1/analytics/summary", requireCapability(deps, authorization.ReportRead, handleAnalyticsSummary(deps)))
	mux.HandleFunc("GET /v1/analytics/rollups", requireCapability(deps, authorization.ReportRead, handleListAnalyticsRollups(deps)))
	mux.HandleFunc("GET /v1/reports", requireCapability(deps, authorization.ReportRead, handleListReports(deps)))
	mux.HandleFunc("POST /v1/reports", requireCapability(deps, authorization.ReportRead, Idempotency(deps)(handleCreateReport(deps))))
	mux.HandleFunc("GET /v1/reports/{id}", requireCapability(deps, authorization.ReportRead, handleGetReport(deps)))
}
func handleListAnalyticsMetrics(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": deps.Analytics.MetricDefinitions()})
	}
}
func handleAnalyticsSummary(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, err := analyticsWindow(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		item, err := deps.Analytics.Summary(r.Context(), actorFromRequest(r).WorkspaceID, from, to)
		if err != nil {
			writeAnalyticsInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func analyticsWindow(r *http.Request) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -30)
	if value := r.URL.Query().Get("from"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("from must be RFC3339")
		}
		from = parsed
	}
	if value := r.URL.Query().Get("to"); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("to must be RFC3339")
		}
		to = parsed
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, errors.New("from must be before to")
	}
	return from, to, nil
}
func handleListAnalyticsRollups(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, err := analyticsWindow(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
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
