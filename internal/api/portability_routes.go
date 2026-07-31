package api

import (
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/portability"
)

func registerPortabilityRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/portability/exports", requireCapability(deps, authorization.WorkspaceManage, handleListExports(deps)))
	mux.HandleFunc("GET /v1/portability/exports/{id}/manifest", requireCapability(deps, authorization.WorkspaceManage, handleExportManifest(deps)))
	mux.HandleFunc("POST /v1/portability/exports", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleCreateExport(deps))))
	mux.HandleFunc("GET /v1/portability/imports", requireCapability(deps, authorization.WorkspaceManage, handleListImports(deps)))
	mux.HandleFunc("POST /v1/portability/imports", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleCreateImport(deps))))
	mux.HandleFunc("POST /v1/portability/imports/{id}/preview", requireCapability(deps, authorization.WorkspaceManage, handlePreviewImport(deps)))
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
			FileID  string         `json:"file_id"`
			Kind    string         `json:"kind"`
			Mapping map[string]any `json:"mapping"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Portability.CreateImport(r.Context(), actor.WorkspaceID, actor.MemberID, input.FileID, input.Kind, input.Mapping)
		if err != nil {
			writePortabilityError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusAccepted, item)
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

func writePortabilityError(w http.ResponseWriter, r *http.Request, err error) {
	httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
}
