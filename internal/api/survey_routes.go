package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/survey"
)

func registerSurveyRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/surveys", requireCapability(deps, authorization.SurveyManage, handleListSurveys(deps)))
	mux.HandleFunc("POST /v1/surveys", requireCapability(deps, authorization.SurveyManage, Idempotency(deps)(handleCreateSurvey(deps))))
	mux.HandleFunc("GET /v1/surveys/{id}", requireCapability(deps, authorization.SurveyManage, handleGetSurvey(deps)))
	mux.HandleFunc("PATCH /v1/surveys/{id}", requireCapability(deps, authorization.SurveyManage, handleUpdateSurvey(deps)))
	mux.HandleFunc("GET /v1/surveys/{id}/responses", requireCapability(deps, authorization.SurveyManage, handleListSurveyResponses(deps)))
	mux.HandleFunc("GET /v1/surveys/{id}/summary", requireCapability(deps, authorization.SurveyManage, handleSurveySummary(deps)))
	mux.HandleFunc("GET /v1/public/surveys/{workspaceID}/{id}", handlePublicGetSurvey(deps))
	mux.HandleFunc("POST /v1/public/surveys/{workspaceID}/{id}/responses", Idempotency(deps)(handlePublicSubmitSurvey(deps)))
}

func handleListSurveys(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := deps.Survey.List(r.Context(), actorFromRequest(r).WorkspaceID)
		if err != nil {
			writeSurveyInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
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
		items, err := deps.Survey.ListResponses(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"), 100)
		if err != nil {
			writeSurveyInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
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
