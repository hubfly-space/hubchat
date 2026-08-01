package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/survey"
)

func registerSurveyRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/surveys", requireCapability(deps, authorization.SurveyManage, handleListSurveys(deps)))
	mux.HandleFunc("POST /v1/surveys", requireCapability(deps, authorization.SurveyManage, Idempotency(deps)(handleCreateSurvey(deps))))
	mux.HandleFunc("GET /v1/surveys/{id}", requireCapability(deps, authorization.SurveyManage, handleGetSurvey(deps)))
	mux.HandleFunc("PATCH /v1/surveys/{id}", requireCapability(deps, authorization.SurveyManage, Idempotency(deps)(handleUpdateSurvey(deps))))
	mux.HandleFunc("GET /v1/surveys/{id}/responses", requireCapability(deps, authorization.SurveyManage, handleListSurveyResponses(deps)))
	mux.HandleFunc("GET /v1/surveys/{id}/responses.csv", requireCapability(deps, authorization.SurveyManage, handleExportSurveyResponses(deps)))
	mux.HandleFunc("GET /v1/surveys/{id}/summary", requireCapability(deps, authorization.SurveyManage, handleSurveySummary(deps)))
	mux.HandleFunc("GET /v1/public/surveys/{workspaceID}/{id}", handlePublicGetSurvey(deps))
	mux.HandleFunc("POST /v1/public/surveys/{workspaceID}/{id}/responses", Idempotency(deps)(handlePublicSubmitSurvey(deps)))
}

func handleListSurveys(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.Survey.ListPage(r.Context(), actorFromRequest(r).WorkspaceID, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeSurveyInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item survey.Survey) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}
func handleCreateSurvey(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input survey.Input
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		item, err := deps.Survey.Create(r.Context(), actorFromRequest(r).WorkspaceID, input)
		if err != nil {
			writeSurveyError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func handleGetSurvey(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Survey.Get(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeSurveyError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleUpdateSurvey(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil || input.Enabled == nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "enabled is required.")
			return
		}
		item, err := deps.Survey.SetEnabled(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), *input.Enabled)
		if err != nil {
			writeSurveyError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handleListSurveyResponses(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.Survey.ListResponsesPage(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeSurveyInternal(w, r)
			return
		}
		page := NewPage(items, limit, func(item survey.Response) Cursor {
			at := time.Time{}
			if item.SubmittedAt != nil {
				at = *item.SubmittedAt
			}
			return Cursor{At: at, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleExportSurveyResponses(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := actorFromRequest(r).WorkspaceID
		if _, err := deps.Survey.Get(r.Context(), workspaceID, r.PathValue("id")); err != nil {
			writeSurveyError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=hubchat-survey-"+r.PathValue("id")+".csv")
		// CSV is streamed after the survey lookup above, so an invalid survey
		// still receives the normal JSON error contract. Once bytes are sent,
		// an error cannot safely be represented as another response body.
		_ = deps.Survey.WriteResponsesCSV(r.Context(), workspaceID, r.PathValue("id"), w)
	}
}
func handleSurveySummary(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Survey.Summary(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeSurveyError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handlePublicGetSurvey(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Survey.Get(r.Context(), r.PathValue("workspaceID"), r.PathValue("id"))
		if err != nil {
			writeSurveyError(w, r, err)
			return
		}
		if !item.Enabled {
			writeSurveyError(w, r, survey.ErrClosed)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}
func handlePublicSubmitSurvey(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input survey.ResponseInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		workspaceID := r.PathValue("workspaceID")
		customerID := portalCustomerForRequest(r, deps, workspaceID)
		item, err := deps.Survey.Submit(r.Context(), workspaceID, r.PathValue("id"), customerID, input)
		if err != nil {
			writeSurveyError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}
func writeSurveyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, survey.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Survey not found.")
	case errors.Is(err, survey.ErrClosed):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "This survey is no longer accepting responses.")
	case errors.Is(err, survey.ErrAlreadyResponded):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, survey.ErrInvalidName), errors.Is(err, survey.ErrInvalidType), errors.Is(err, survey.ErrInvalidQuestion):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		writeSurveyInternal(w, r)
	}
}
func writeSurveyInternal(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load survey.")
}
