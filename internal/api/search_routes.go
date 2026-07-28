package api

import (
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
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}

		results, err := deps.Search.Search(r.Context(), actor.WorkspaceID, query.Get("q"), limit)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Search failed.")
			return
		}

		out := make([]map[string]any, len(results))
		for i, res := range results {
			out[i] = searchResultJSON(res)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
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
