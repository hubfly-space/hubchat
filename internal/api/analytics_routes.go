package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/jackc/pgx/v5"
)

func registerAnalyticsRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/analytics/metrics", requireCapability(deps, authorization.ReportRead, handleListAnalyticsMetrics(deps)))
	mux.HandleFunc("GET /v1/analytics/summary", requireCapability(deps, authorization.ReportRead, handleAnalyticsSummary(deps)))
	mux.HandleFunc("GET /v1/analytics/rollups", requireCapability(deps, authorization.ReportRead, handleListAnalyticsRollups(deps)))
	mux.HandleFunc("GET /v1/reports", requireCapability(deps, authorization.ReportRead, handleListReports(deps)))
	mux.HandleFunc("POST /v1/reports", requireCapability(deps, authorization.ReportRead, Idempotency(deps)(handleCreateReport(deps))))
	mux.HandleFunc("GET /v1/reports/{id}", requireCapability(deps, authorization.ReportRead, handleGetReport(deps)))
	mux.HandleFunc("GET /v1/reports/{id}/schedules", requireCapability(deps, authorization.ReportRead, handleListReportSchedules(deps)))
	mux.HandleFunc("POST /v1/reports/{id}/schedules", requireCapability(deps, authorization.ReportRead, Idempotency(deps)(handleCreateReportSchedule(deps))))
	mux.HandleFunc("PATCH /v1/reports/{id}/schedules/{scheduleID}", requireCapability(deps, authorization.ReportRead, Idempotency(deps)(handleUpdateReportSchedule(deps))))
	mux.HandleFunc("DELETE /v1/reports/{id}/schedules/{scheduleID}", requireCapability(deps, authorization.ReportRead, Idempotency(deps)(handleDeleteReportSchedule(deps))))
	mux.HandleFunc("GET /v1/analytics/export.csv", requireCapability(deps, authorization.ReportRead, handleAnalyticsExport(deps)))
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
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.Analytics.ListReportsPage(r.Context(), actorFromRequest(r).WorkspaceID, cursor.Value, cursor.ID, limit+1)
		if err != nil {
			writeAnalyticsInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item analytics.Report) Cursor {
			return Cursor{Value: item.Name, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
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

func handleListReportSchedules(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		actor := actorFromRequest(r)
		items, err := deps.Analytics.ListSchedulesPage(r.Context(), actor.WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeAnalyticsInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item analytics.ReportSchedule) Cursor { return Cursor{At: item.CreatedAt, ID: item.ID} })
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleCreateReportSchedule(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input analytics.ScheduleInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		input.ReportID = r.PathValue("id")
		item, err := deps.Analytics.CreateSchedule(r.Context(), actorFromRequest(r).WorkspaceID, input, time.Now().UTC())
		if err != nil {
			writeAnalyticsScheduleError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}

func handleUpdateReportSchedule(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input analytics.ScheduleInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		input.ReportID = r.PathValue("id")
		item, err := deps.Analytics.UpdateSchedule(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("scheduleID"), input, time.Now().UTC())
		if err != nil {
			writeAnalyticsScheduleError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleDeleteReportSchedule(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := deps.Analytics.DeleteSchedule(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("scheduleID")); err != nil {
			writeAnalyticsScheduleError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAnalyticsExport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, err := analyticsWindow(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		metrics := strings.Split(r.URL.Query().Get("metrics"), ",")
		body, err := deps.Analytics.ExportCSV(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("report_id"), metrics, from, to)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
			return
		}
		name := "hubchat-analytics-" + strconv.FormatInt(time.Now().Unix(), 10) + ".csv"
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

func writeAnalyticsScheduleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, analytics.ErrInvalidScheduleReport), errors.Is(err, analytics.ErrInvalidScheduleCadence), errors.Is(err, analytics.ErrInvalidScheduleRecipients), errors.Is(err, analytics.ErrInvalidScheduleFormat), errors.Is(err, analytics.ErrInvalidScheduleOptions):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, analytics.ErrInvalidReportName):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		if errors.Is(err, pgx.ErrNoRows) {
			httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Report schedule not found.")
			return
		}
		writeAnalyticsInternal(w, r)
	}
}
func writeAnalyticsInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load analytics.")
}
