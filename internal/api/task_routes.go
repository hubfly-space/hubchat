package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/task"
)

func registerTaskRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/tasks", requireCapability(deps, authorization.TaskManage, handleListTasks(deps)))
	mux.HandleFunc("POST /v1/tasks", requireCapability(deps, authorization.TaskManage, Idempotency(deps)(handleCreateTask(deps))))
	mux.HandleFunc("GET /v1/tasks/{id}", requireCapability(deps, authorization.TaskManage, handleGetTask(deps)))
	mux.HandleFunc("PATCH /v1/tasks/{id}", requireCapability(deps, authorization.TaskManage, Idempotency(deps)(handleUpdateTask(deps))))
}

func handleListTasks(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed task cursor.")
			return
		}
		overdue := false
		if raw := r.URL.Query().Get("overdue"); raw != "" {
			overdue, err = strconv.ParseBool(raw)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "overdue must be a boolean.")
				return
			}
		}
		actor := actorFromRequest(r)
		items, err := deps.Task.ListPage(r.Context(), actor.WorkspaceID,
			r.URL.Query().Get("state"), r.URL.Query().Get("assignee_id"),
			r.URL.Query().Get("q"), overdue, cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeTaskError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(items, limit, func(item task.Task) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		}))
	}
}

func handleCreateTask(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input task.Input
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Task.Create(r.Context(), actor.WorkspaceID, actor.MemberID, input)
		if err != nil {
			writeTaskError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}

func handleGetTask(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		item, err := deps.Task.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeTaskError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleUpdateTask(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input task.UpdateInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Task.Update(r.Context(), actor.WorkspaceID, r.PathValue("id"), input)
		if err != nil {
			writeTaskError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func writeTaskError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, task.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "The task was not found.")
	case errors.Is(err, task.ErrInvalidTitle), errors.Is(err, task.ErrInvalidState), errors.Is(err, task.ErrInvalidSubject), errors.Is(err, task.ErrInvalidAssignee):
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "The task could not be loaded.")
	}
}
