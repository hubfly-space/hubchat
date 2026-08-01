package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/search"
)

// registerSearchRoutes mounts the CommandPalette's one query endpoint.
func registerSearchRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/search",
		requireCapability(deps, authorization.ConversationRead, handleSearch(deps)))
}

func handleSearch(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		limit := 10
		if raw := query.Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed search limit.")
				return
			}
			limit = parsed
		}
		if limit <= 0 {
			limit = 10
		}
		if limit > 100 {
			limit = 100
		}

		page, err := deps.Search.SearchPage(r.Context(), actor.WorkspaceID, query.Get("q"), query.Get("cursor"), limit)
		if err != nil {
			if errors.Is(err, search.ErrBadCursor) {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed search cursor.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Search failed.")
			return
		}

		out := make([]map[string]any, 0, len(page.Results))
		for _, res := range page.Results {
			out = append(out, searchResultJSON(res))
		}
		var nextCursor *string
		if page.NextCursor != "" {
			nextCursor = &page.NextCursor
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: nextCursor, HasMore: page.HasMore})
	}
}

func searchResultJSON(r search.Result) map[string]any {
	return map[string]any{
		"kind":            r.Kind,
		"title":           r.Title,
		"snippet":         r.Snippet,
		"entity_id":       r.EntityID,
		"conversation_id": nullableString(r.ConversationID),
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
