package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/jobs"
)

func registerJobRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/jobs", requireCapability(deps, authorization.WorkspaceManage, handleListJobs(deps)))
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleCancelJob(deps))))
	mux.HandleFunc("POST /v1/jobs/{id}/retry", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleRetryJob(deps))))
}

func handleListJobs(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		items, err := deps.Jobs.List(r.Context(), jobs.ListFilter{WorkspaceID: actorFromRequest(r).WorkspaceID, State: jobs.State(query.Get("state")), Queue: query.Get("queue"), Limit: 200})
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load background jobs.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": items})
	}
}

func handleRetryJob(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Jobs.Retry(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			if errors.Is(err, jobs.ErrNotFound) {
				httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Job not found or is not retryable.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not retry background job.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"status": "queued"})
	}
}

func handleCancelJob(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Jobs.Cancel(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			if errors.Is(err, jobs.ErrNotFound) {
				httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Job not found or is no longer pending.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not cancel background job.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
	}
}
