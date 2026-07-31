package api

import (
	"net/http"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/portability"
)

func registerPortabilityRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/portability/exports", requireCapability(deps, authorization.WorkspaceManage, handleListExports(deps)))
	mux.HandleFunc("GET /v1/portability/exports/{id}", requireCapability(deps, authorization.WorkspaceManage, handleGetExport(deps)))
	mux.HandleFunc("GET /v1/portability/exports/{id}/manifest", requireCapability(deps, authorization.WorkspaceManage, handleExportManifest(deps)))
	mux.HandleFunc("POST /v1/portability/exports", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleCreateExport(deps))))
	mux.HandleFunc("GET /v1/portability/imports", requireCapability(deps, authorization.WorkspaceManage, handleListImports(deps)))
	mux.HandleFunc("GET /v1/portability/imports/{id}", requireCapability(deps, authorization.WorkspaceManage, handleGetImport(deps)))
	mux.HandleFunc("POST /v1/portability/import-files", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleUploadPortabilityFile(deps))))
	mux.HandleFunc("POST /v1/portability/imports", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleCreateImport(deps))))
	mux.HandleFunc("POST /v1/portability/imports/{id}/preview", requireCapability(deps, authorization.WorkspaceManage, handlePreviewImport(deps)))
	mux.HandleFunc("POST /v1/portability/imports/{id}/confirm", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleConfirmImport(deps))))
}

func handleUploadPortabilityFile(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if deps.File == nil {
			httpserver.WriteError(w, r, http.StatusServiceUnavailable, httpserver.CodeUnavailable, "File storage is unavailable.")
			return
		}
		created, err := uploadFormFile(r, deps.File, actor.WorkspaceID, "user", actor.MemberID)
		if err != nil {
			writeFileError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, fileJSON(*created))
	}
}

func handleGetExport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Portability.Get(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleExportManifest(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		manifest, err := deps.Portability.ExportManifest(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, manifest)
	}
}

func handleListExports(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.Portability.ListPage(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("state"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		page := NewPage(items, limit, func(item portability.Request) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleCreateExport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Kind  string         `json:"kind"`
			Scope map[string]any `json:"scope"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Portability.CreateExport(r.Context(), actor.WorkspaceID, actor.MemberID, input.Kind, input.Scope)
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusAccepted, item)
	}
}

func handleListImports(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		items, err := deps.Portability.ListImportsPage(r.Context(), actorFromRequest(r).WorkspaceID, r.URL.Query().Get("state"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		page := NewPage(items, limit, func(item portability.Request) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

func handleCreateImport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			FileID    string         `json:"file_id"`
			Kind      string         `json:"kind"`
			Mapping   map[string]any `json:"mapping"`
			AutoStart bool           `json:"auto_start"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Portability.CreateImport(r.Context(), actor.WorkspaceID, actor.MemberID, input.FileID, input.Kind, input.Mapping, input.AutoStart)
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusAccepted, item)
	}
}

func handleGetImport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := deps.Portability.GetImport(r.Context(), actorFromRequest(r).WorkspaceID, r.PathValue("id"))
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handlePreviewImport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		summaries, err := deps.Portability.PreviewImport(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": summaries})
	}
}

func handleConfirmImport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			BackupVerified bool `json:"backup_verified"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Portability.ConfirmImport(r.Context(), actor.WorkspaceID, r.PathValue("id"), input.BackupVerified)
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusAccepted, item)
	}
}

func writePortabilityError(w http.ResponseWriter, r *http.Request, err error) {
	message := err.Error()
	switch {
	case strings.Contains(message, "not found"):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, message)
	case strings.Contains(message, "already") || strings.Contains(message, "already "):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, message)
	case strings.Contains(message, "job queue is unavailable"):
		httpserver.WriteError(w, r, http.StatusServiceUnavailable, httpserver.CodeUnavailable, "The background job queue is unavailable.")
	case strings.HasPrefix(message, "portability:"):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, message)
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not complete the portability request.")
	}
}
