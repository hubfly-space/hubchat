package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerAuditRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/audit-logs",
		requireCapability(deps, authorization.AuditRead, handleListAuditLogs(deps)))
}

type auditLogJSON struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	ActorType   string         `json:"actor_type"`
	ActorID     string         `json:"actor_id,omitempty"`
	ActorName   string         `json:"actor_name"`
	Action      string         `json:"action"`
	EntityType  string         `json:"entity_type,omitempty"`
	EntityID    string         `json:"entity_id,omitempty"`
	RequestID   string         `json:"request_id,omitempty"`
	IP          string         `json:"ip,omitempty"`
	Metadata    map[string]any `json:"metadata"`
	OccurredAt  string         `json:"occurred_at"`
}

// handleListAuditLogs serves the audit log screen (§6.19), read-only —
// audit entries are append-only and nothing in this API ever edits or
// deletes one.
//
// Pagination is cursor-based on (occurred_at, id) rather than an offset, per
// §16: an append-only log paged by offset shifts under a reader as new
// entries arrive at the front.
func handleListAuditLogs(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		filter := audit.Filter{
			ActorID:    query.Get("actor_id"),
			Action:     audit.Action(query.Get("action")),
			EntityType: query.Get("entity_type"),
			EntityID:   query.Get("entity_id"),
		}

		if limit := query.Get("limit"); limit != "" {
			if parsed, err := strconv.Atoi(limit); err == nil {
				filter.Limit = parsed
			}
		}

		if cursor := query.Get("cursor"); cursor != "" {
			decoded, err := DecodeCursor(cursor)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
				return
			}
			filter.Before = decoded.At
			filter.BeforeID = decoded.ID
		}

		// Queried at limit+1 so NewPage can tell "there is another page" from
		// one extra row rather than a second count query.
		limit := pageLimit(filter.Limit)
		filter.Limit = limit + 1

		records, err := deps.Audit.List(r.Context(), actor.WorkspaceID, filter)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load the audit log.")
			return
		}

		page := NewPage(records, limit, func(r audit.Record) Cursor {
			return Cursor{At: r.OccurredAt, ID: r.ID}
		})

		entries := make([]auditLogJSON, 0, len(page.Data))
		for _, record := range page.Data {
			entries = append(entries, auditRecordJSON(record))
		}

		httpserver.WriteJSON(w, http.StatusOK, Page[auditLogJSON]{
			Data: entries, NextCursor: page.NextCursor, HasMore: page.HasMore,
		})
	}
}

// pageLimit mirrors the default/clamp behaviour PageParams applies, for the
// one caller here that builds its filter from raw query values instead of
// going through PageParams directly (it also reads actor/action/entity
// filters PageParams knows nothing about).
func pageLimit(requested int) int {
	const (
		defaultLimit = 50
		maxLimit     = 200
	)
	if requested <= 0 {
		return defaultLimit
	}
	if requested > maxLimit {
		return maxLimit
	}
	return requested
}

func auditRecordJSON(r audit.Record) auditLogJSON {
	return auditLogJSON{
		ID: r.ID, WorkspaceID: r.WorkspaceID, ActorType: string(r.ActorType),
		ActorID: r.ActorID, ActorName: r.ActorName, Action: string(r.Action),
		EntityType: r.EntityType, EntityID: r.EntityID, RequestID: r.RequestID, IP: r.IP,
		Metadata:   decodeMetadata(r.Metadata),
		OccurredAt: r.OccurredAt.UTC().Format(time.RFC3339),
	}
}

func decodeMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}
