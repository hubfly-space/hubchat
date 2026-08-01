package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerAuditRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/audit-logs",
		requireCapability(deps, authorization.AuditRead, handleListAuditLogs(deps)))
	mux.HandleFunc("GET /v1/audit-logs/export.csv",
		requireCapability(deps, authorization.AuditRead, handleExportAuditLogs(deps)))
}

func handleExportAuditLogs(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		filter := audit.Filter{ActorID: query.Get("actor_id"), Action: audit.Action(query.Get("action")), EntityType: query.Get("entity_type"), EntityID: query.Get("entity_id")}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="hubchat-audit-log.csv"`)
		if err := deps.Audit.WriteCSV(r.Context(), actorFromRequest(r).WorkspaceID, filter, w); err != nil {
			// WriteCSV queries before writing the header. If a database failure
			// occurs after streaming starts, the only safe response is a closed
			// partial download; the request logger still records its request id.
			return
		}
	}
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

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		filter.Before = cursor.At
		filter.BeforeID = cursor.ID
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
