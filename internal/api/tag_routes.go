package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

func registerTagRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/tags",
		requireActor(deps, handleListTags(deps)))
	mux.HandleFunc("POST /v1/tags",
		requireCapability(deps, authorization.WorkspaceManage, idempotent(handleCreateTag(deps))))
	mux.HandleFunc("DELETE /v1/tags/{id}",
		requireCapability(deps, authorization.WorkspaceManage, idempotent(handleDeleteTag(deps))))
	mux.HandleFunc("POST /v1/tags/{id}/merge",
		requireCapability(deps, authorization.WorkspaceManage, idempotent(handleMergeTag(deps))))
}

func handleListTags(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed tag cursor.")
			return
		}
		tags, err := deps.Workspace.ListTagsPage(r.Context(), actor.WorkspaceID, r.URL.Query().Get("q"), cursor.Value, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load tags.")
			return
		}
		page := NewPage(tags, limit, func(tag workspace.Tag) Cursor { return Cursor{Value: tag.Name, ID: tag.ID} })
		httpserver.WriteJSON(w, http.StatusOK, Page[tagWithUsageJSON]{Data: tagsWithUsageJSON(actor.WorkspaceID, page.Data), NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

type tagWithUsageJSON struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Color       int    `json:"color"`
	UsageCount  int    `json:"usage_count"`
}

func tagsWithUsageJSON(workspaceID string, tags []workspace.Tag) []tagWithUsageJSON {
	out := make([]tagWithUsageJSON, 0, len(tags))
	for _, tag := range tags {
		out = append(out, tagWithUsageJSON{
			ID: tag.ID, WorkspaceID: workspaceID, Name: tag.Name, Color: tag.Color, UsageCount: tag.UsageCount,
		})
	}
	return out
}

type createTagRequest struct {
	Name  string `json:"name"`
	Color int    `json:"color"`
}

func handleCreateTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req createTagRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		tag, err := deps.Workspace.CreateTag(r.Context(), actor.WorkspaceID, actor.MemberID, req.Name, req.Color)
		if err != nil {
			writeTagError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, tagWithUsageJSON{
			ID: tag.ID, WorkspaceID: actor.WorkspaceID, Name: tag.Name, Color: tag.Color,
		})
	}
}

func handleDeleteTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Workspace.DeleteTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeTagError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type mergeTagRequest struct {
	IntoTagID string `json:"into_tag_id"`
}

func handleMergeTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req mergeTagRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		err := deps.Workspace.MergeTags(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.IntoTagID)
		if err != nil {
			writeTagError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeTagError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrInvalidTagName), errors.Is(err, workspace.ErrInvalidColor):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, workspace.ErrTagNameTaken):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, workspace.ErrTagNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "No such tag.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
