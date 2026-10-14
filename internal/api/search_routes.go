package api

import (
	"errors"
	"github.com/hubchat/hubchat/internal/searchmodel"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/search"
)

// registerSearchRoutes mounts the CommandPalette's one query endpoint.
func registerSearchRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/search",
		requireActor(deps, handleSearch(deps)))
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

		filters := searchmodel.Filters{Channel: query.Get("channel"), TagID: query.Get("tag_id")}
		if kind := query.Get("kind"); kind != "" {
			filters.Kinds = strings.Split(kind, ",")
			for _, value := range filters.Kinds {
				if value != "message" && value != "customer" && value != "company" && value != "ticket" && value != "article" && value != "feedback" {
					httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Unknown search kind.")
					return
				}
			}
		}
		for key, destination := range map[string]**time.Time{"after": &filters.After, "before": &filters.Before} {
			if value := query.Get(key); value != "" {
				at, err := time.Parse(time.RFC3339, value)
				if err != nil {
					httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Search dates must use RFC3339.")
					return
				}
				*destination = &at
			}
		}

		page, err := deps.Search.SearchFilteredPage(r.Context(), actor.WorkspaceID, query.Get("q"), query.Get("cursor"), limit, filters)
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
