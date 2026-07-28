package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/httpserver"
)

// registerCustomerRoutes mounts the customer surface: profile CRUD, tags,
// blocking, the metadata allowlist and its validated read/write path, event
// ingestion and timeline, contact sessions, export/delete, and identity
// merge (§6.9, §6.10, §26.3, §26.4).
func registerCustomerRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/customers",
		requireCapability(deps, authorization.CustomerRead, handleSearchCustomers(deps)))
	mux.HandleFunc("GET /v1/customers/{id}",
		requireCapability(deps, authorization.CustomerRead, handleGetCustomer(deps)))
	mux.HandleFunc("PATCH /v1/customers/{id}",
		requireCapability(deps, authorization.CustomerRead, handleUpdateCustomer(deps)))
	mux.HandleFunc("PATCH /v1/customers/{id}/owner",
		requireCapability(deps, authorization.CustomerRead, handleSetCustomerOwner(deps)))
	mux.HandleFunc("DELETE /v1/customers/{id}",
		requireCapability(deps, authorization.CustomerReadSensitive, handleDeleteCustomer(deps)))

	mux.HandleFunc("POST /v1/customers/{id}/tags",
		requireCapability(deps, authorization.CustomerRead, handleAddCustomerTag(deps)))
	mux.HandleFunc("DELETE /v1/customers/{id}/tags/{tagID}",
		requireCapability(deps, authorization.CustomerRead, handleRemoveCustomerTag(deps)))

	mux.HandleFunc("PATCH /v1/customers/{id}/attributes",
		requireCapability(deps, authorization.CustomerRead, handleSetCustomerAttributes(deps)))
	mux.HandleFunc("POST /v1/customers/{id}/attributes/{key}/reveal",
		requireCapability(deps, authorization.CustomerReadSensitive, handleRevealCustomerAttribute(deps)))

	mux.HandleFunc("GET /v1/customers/{id}/timeline",
		requireCapability(deps, authorization.CustomerRead, handleCustomerTimeline(deps)))
	mux.HandleFunc("GET /v1/customers/{id}/sessions",
		requireCapability(deps, authorization.CustomerRead, handleCustomerSessions(deps)))
	mux.HandleFunc("GET /v1/customers/{id}/export",
		requireCapability(deps, authorization.CustomerReadSensitive, handleExportCustomer(deps)))

	mux.HandleFunc("POST /v1/customers/merge/preview",
		requireCapability(deps, authorization.CustomerMerge, handlePreviewMerge(deps)))
	mux.HandleFunc("POST /v1/customers/merge",
		requireCapability(deps, authorization.CustomerMerge, handleMergeCustomers(deps)))
	mux.HandleFunc("POST /v1/customers/merges/{id}/reverse",
		requireCapability(deps, authorization.CustomerMerge, handleReverseMerge(deps)))

	mux.HandleFunc("POST /v1/blocked-contacts",
		requireCapability(deps, authorization.ConversationDelete, handleBlockContact(deps)))

	mux.HandleFunc("POST /v1/events",
		requireCapability(deps, authorization.CustomerRead, handleIngestEvents(deps)))
	mux.HandleFunc("GET /v1/events",
		requireCapability(deps, authorization.CustomerRead, handleListEvents(deps)))

	mux.HandleFunc("GET /v1/attribute-definitions",
		requireCapability(deps, authorization.CustomerRead, handleListAttributeDefinitions(deps)))
	mux.HandleFunc("POST /v1/attribute-definitions",
		requireCapability(deps, authorization.WorkspaceManage, handleCreateAttributeDefinition(deps)))
	mux.HandleFunc("PATCH /v1/attribute-definitions/{id}",
		requireCapability(deps, authorization.WorkspaceManage, handleUpdateAttributeDefinition(deps)))
	mux.HandleFunc("DELETE /v1/attribute-definitions/{id}",
		requireCapability(deps, authorization.WorkspaceManage, handleArchiveAttributeDefinition(deps)))
}

// customerJSON masks sensitive attribute values (per their attribute
// definition) unless the caller holds customer.read_sensitive — masking
// happens even for a reader who could otherwise see everything, because
// §12's audit-on-reveal only means something if seeing the real value is a
// distinct, logged action from seeing the record at all. maskedKeys names
// which attributes were masked, so the UI can render a "Reveal" affordance
// specifically for those rather than guessing from a null value.
func customerJSON(c customer.Customer, tagIDs, companyIDs []string, sensitiveKeys map[string]bool, canReveal bool) map[string]any {
	attributes := c.Attributes
	maskedKeys := []string{}
	if !canReveal && len(sensitiveKeys) > 0 {
		attributes = make(map[string]any, len(c.Attributes))
		for k, v := range c.Attributes {
			if sensitiveKeys[k] {
				maskedKeys = append(maskedKeys, k)
				continue
			}
			attributes[k] = v
		}
	}

	return map[string]any{
		"id":                    c.ID,
		"workspace_id":          c.WorkspaceID,
		"name":                  c.Name,
		"email":                 c.Email,
		"phone":                 c.Phone,
		"avatar_url":            c.AvatarURL,
		"external_id":           c.ExternalID,
		"verification":          c.Verification,
		"company_ids":           orEmpty(companyIDs),
		"language":              c.Language,
		"timezone":              c.Timezone,
		"attributes":            attributes,
		"masked_attribute_keys": orEmpty(maskedKeys),
		"tag_ids":               orEmpty(tagIDs),
		"owner_id":              c.OwnerID,
		// Presence and current_url describe a live widget/portal session
		// (Stage 5/6). Until that exists there is no session to report, so
		// these are always their "nobody is here" values rather than a guess.
		"presence":          "offline",
		"current_url":       nil,
		"first_seen_at":     c.FirstSeenAt,
		"last_seen_at":      c.LastSeenAt,
		"last_contacted_at": c.LastContactedAt,
		"version":           c.Version,
	}
}

func loadCustomerJSON(r *http.Request, deps Deps, workspaceID string, c customer.Customer) map[string]any {
	actor := actorFromRequest(r)

	tagIDs, err := deps.Customer.Tags(r.Context(), workspaceID, c.ID)
	if err != nil {
		tagIDs = []string{}
	}
	companyIDs, err := deps.Customer.CompanyIDs(r.Context(), workspaceID, c.ID)
	if err != nil {
		companyIDs = []string{}
	}
	sensitiveKeys := sensitiveAttributeKeys(r, deps, workspaceID)
	return customerJSON(c, tagIDs, companyIDs, sensitiveKeys, actor.Can(authorization.CustomerReadSensitive))
}

func sensitiveAttributeKeys(r *http.Request, deps Deps, workspaceID string) map[string]bool {
	defs, err := deps.Customer.ListAttributeDefinitions(r.Context(), workspaceID, "customer")
	if err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(defs))
	for _, d := range defs {
		if d.Sensitive {
			out[d.Key] = true
		}
	}
	return out
}

func handleGetCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		c, err := deps.Customer.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCustomerJSON(r, deps, actor.WorkspaceID, *c))
	}
}

// handleSearchCustomers serves three call shapes on one path, distinguished
// by query params: ids= is a batch lookup (a page of rows that each
// reference a customer), cursor/tag_id/company_id/verification select the
// server-driven paginated directory (CustomerList), and a bare q= (or
// nothing) stays the small unpaginated picker search every other page's
// customer-lookup already relies on. All three return the same {data: [...]}
// shape a picker only ever reads .data from, so none of the existing callers
// notice the directory case also sets next_cursor/has_more.
func handleSearchCustomers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		if idsParam := query.Get("ids"); idsParam != "" {
			ids := strings.Split(idsParam, ",")
			customers, err := deps.Customer.GetMany(r.Context(), actor.WorkspaceID, ids)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load customers.")
				return
			}
			httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": customerJSONList(r, deps, actor.WorkspaceID, customers)})
			return
		}

		isDirectory := query.Has("cursor") || query.Get("tag_id") != "" || query.Get("company_id") != "" || query.Get("verification") != ""
		if !isDirectory {
			limit := 20
			if raw := query.Get("limit"); raw != "" {
				if parsed, parseErr := strconv.Atoi(raw); parseErr == nil {
					limit = parsed
				}
			}
			customers, err := deps.Customer.Search(r.Context(), actor.WorkspaceID, query.Get("q"), limit)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load customers.")
				return
			}
			httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": customerJSONList(r, deps, actor.WorkspaceID, customers)})
			return
		}

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		customers, err := deps.Customer.List(r.Context(), actor.WorkspaceID, customer.ListFilter{
			Query: query.Get("q"), TagID: query.Get("tag_id"), CompanyID: query.Get("company_id"),
			Verification: query.Get("verification"), Before: cursor.At, BeforeID: cursor.ID, Limit: limit + 1,
		})
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load customers.")
			return
		}
		page := NewPage(customers, limit, func(c customer.Customer) Cursor {
			at := c.FirstSeenAt
			if c.LastSeenAt != nil {
				at = *c.LastSeenAt
			}
			return Cursor{At: at, ID: c.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{
			Data: customerJSONList(r, deps, actor.WorkspaceID, page.Data), NextCursor: page.NextCursor, HasMore: page.HasMore,
		})
	}
}

func customerJSONList(r *http.Request, deps Deps, workspaceID string, customers []customer.Customer) []map[string]any {
	out := make([]map[string]any, len(customers))
	for i, c := range customers {
		out[i] = loadCustomerJSON(r, deps, workspaceID, c)
	}
	return out
}

type updateCustomerRequest struct {
	Version  int     `json:"version"`
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"`
	Language *string `json:"language"`
	Timezone *string `json:"timezone"`
}

func handleUpdateCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req updateCustomerRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		c, err := deps.Customer.Update(
			r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Version,
			req.Name, req.Email, req.Phone, req.Language, req.Timezone,
		)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCustomerJSON(r, deps, actor.WorkspaceID, *c))
	}
}

type addCustomerTagRequest struct {
	TagID string `json:"tag_id"`
}

func handleAddCustomerTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req addCustomerTagRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Customer.AddTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TagID); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveCustomerTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Customer.RemoveTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("tagID")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type setCustomerOwnerRequest struct {
	OwnerID *string `json:"owner_id"`
}

func handleSetCustomerOwner(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setCustomerOwnerRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		c, err := deps.Customer.SetOwner(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.OwnerID)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCustomerJSON(r, deps, actor.WorkspaceID, *c))
	}
}

func handleDeleteCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Customer.Delete(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type setAttributesRequest struct {
	Attributes map[string]any `json:"attributes"`
}

// handleSetCustomerAttributes is the authenticated write path (§6.10):
// source is always "rest_api" here — the dashboard's own API is what that
// source name means — and every key is validated against the metadata
// schema before anything is written.
func handleSetCustomerAttributes(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setAttributesRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		c, err := deps.Customer.SetCustomerAttributes(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), "rest_api", req.Attributes)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadCustomerJSON(r, deps, actor.WorkspaceID, *c))
	}
}

// handleRevealCustomerAttribute returns one sensitive attribute's real value
// and records the access — §12's "audited on reveal". Gated on
// customer.read_sensitive independently of the general customer read path,
// so seeing a masked record and revealing one field inside it are two
// separately-authorized, separately-logged actions.
func handleRevealCustomerAttribute(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		key := r.PathValue("key")

		c, err := deps.Customer.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		if err := deps.Audit.Record(r.Context(), audit.Entry{
			WorkspaceID: actor.WorkspaceID, ActorType: audit.ActorUser, ActorID: actor.MemberID,
			Action: "customer.attribute_revealed", EntityType: "customer", EntityID: c.ID,
			Metadata: map[string]any{"key": key},
		}); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not record the reveal.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"key": key, "value": c.Attributes[key]})
	}
}

func handleCustomerTimeline(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		evts, err := deps.Customer.Timeline(r.Context(), actor.WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load the timeline.")
			return
		}
		page := NewPage(evts, limit, func(e customer.CustomerEvent) Cursor { return Cursor{At: e.OccurredAt, ID: e.ID} })
		out := make([]map[string]any, len(page.Data))
		for i, e := range page.Data {
			out[i] = customerEventJSON(e)
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleCustomerSessions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		sessions, err := deps.Customer.Sessions(r.Context(), actor.WorkspaceID, r.PathValue("id"), 20)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load sessions.")
			return
		}
		out := make([]map[string]any, len(sessions))
		for i, s := range sessions {
			out[i] = contactSessionJSON(s)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

func handleExportCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		bundle, err := deps.Customer.Export(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		if err := deps.Audit.Record(r.Context(), audit.Entry{
			WorkspaceID: actor.WorkspaceID, ActorType: audit.ActorUser, ActorID: actor.MemberID,
			Action: "customer.exported", EntityType: "customer", EntityID: r.PathValue("id"),
		}); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not export this customer.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("id")+`-export.json"`)
		httpserver.WriteJSON(w, http.StatusOK, bundle)
	}
}

type mergeCustomersRequest struct {
	WinnerID string `json:"winner_id"`
	LoserID  string `json:"loser_id"`
}

func handlePreviewMerge(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req mergeCustomersRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		preview, err := deps.Customer.PreviewMerge(r.Context(), actor.WorkspaceID, req.WinnerID, req.LoserID)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"conversation_count": preview.ConversationCount, "ticket_count": preview.TicketCount,
			"tag_count": preview.TagCount, "company_count": preview.CompanyCount,
		})
	}
}

func handleMergeCustomers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req mergeCustomersRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		record, err := deps.Customer.Merge(r.Context(), actor.WorkspaceID, actor.MemberID, req.WinnerID, req.LoserID)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, mergeRecordJSON(*record))
	}
}

func handleReverseMerge(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Customer.ReverseMerge(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func mergeRecordJSON(m customer.MergeRecord) map[string]any {
	return map[string]any{
		"id": m.ID, "winner_id": m.WinnerID, "loser_id": m.LoserID, "moved_counts": m.MovedCounts,
		"reversible_until": m.ReversibleUntil, "reversed_at": m.ReversedAt, "created_at": m.CreatedAt,
	}
}

func contactSessionJSON(s customer.ContactSession) map[string]any {
	device := "unknown"
	if s.Device != nil && (*s.Device == "desktop" || *s.Device == "mobile" || *s.Device == "tablet") {
		device = *s.Device
	}
	return map[string]any{
		"id": s.ID, "customer_id": s.CustomerID, "visitor_id": s.VisitorID,
		"started_at": s.StartedAt, "last_seen_at": s.LastSeenAt, "ended_at": s.EndedAt,
		"ip_country": s.IPCountry, "browser": s.Browser, "os": s.OS, "device": device,
		"referrer": s.Referrer, "current_url": s.CurrentURL, "page_views": s.PageViews,
	}
}

func customerEventJSON(e customer.CustomerEvent) map[string]any {
	return map[string]any{
		"id": e.ID, "workspace_id": e.WorkspaceID, "customer_id": e.CustomerID, "session_id": e.SessionID,
		"type": e.Type, "source": e.Source, "url": e.URL, "payload": e.Payload, "occurred_at": e.OccurredAt,
	}
}

type ingestEventItem struct {
	Type    string         `json:"type"`
	Source  string         `json:"source"`
	URL     *string        `json:"url"`
	Payload map[string]any `json:"payload"`
}

type ingestEventsRequest struct {
	CustomerID string            `json:"customer_id"`
	Events     []ingestEventItem `json:"events"`
}

// handleIngestEvents is the authenticated batched event-ingestion path
// (§6.10, §26.4). The unauthenticated widget/SDK path arrives with Stage 5's
// visitor tokens; today this serves server-side integrations and the
// dashboard's own instrumentation, both of which authenticate as an agent.
func handleIngestEvents(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req ingestEventsRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		out := make([]map[string]any, 0, len(req.Events))
		for _, item := range req.Events {
			source := item.Source
			if source == "" {
				source = "rest_api"
			}
			evt, err := deps.Customer.IngestEvent(r.Context(), actor.WorkspaceID, req.CustomerID, item.Type, source, item.URL, item.Payload)
			if err != nil {
				writeCustomerError(w, r, err)
				return
			}
			out = append(out, customerEventJSON(*evt))
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"data": out})
	}
}

func handleListEvents(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		evts, err := deps.Customer.ListEvents(r.Context(), actor.WorkspaceID, r.URL.Query().Get("type"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load events.")
			return
		}
		page := NewPage(evts, limit, func(e customer.CustomerEvent) Cursor { return Cursor{At: e.OccurredAt, ID: e.ID} })
		out := make([]map[string]any, len(page.Data))
		for i, e := range page.Data {
			out[i] = customerEventJSON(e)
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleListAttributeDefinitions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		entityType := r.URL.Query().Get("entity_type")
		if entityType == "" {
			entityType = "customer"
		}
		defs, err := deps.Customer.ListAttributeDefinitions(r.Context(), actor.WorkspaceID, entityType)
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		out := make([]map[string]any, len(defs))
		for i, d := range defs {
			out[i] = attributeDefinitionJSON(d)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type attributeDefinitionRequest struct {
	EntityType         string                        `json:"entity_type"`
	Key                string                        `json:"key"`
	Label              string                        `json:"label"`
	Type               string                        `json:"type"`
	Description        *string                       `json:"description"`
	Options            []string                      `json:"options"`
	AllowedSources     []string                      `json:"allowed_sources"`
	RequiredCapability *string                       `json:"required_capability"`
	Sensitive          bool                          `json:"sensitive"`
	Searchable         bool                          `json:"searchable"`
	Validation         *customer.AttributeValidation `json:"validation"`
	RetentionDays      *int                          `json:"retention_days"`
}

func handleCreateAttributeDefinition(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req attributeDefinitionRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		entityType := req.EntityType
		if entityType == "" {
			entityType = "customer"
		}
		def, err := deps.Customer.CreateAttributeDefinition(r.Context(), actor.WorkspaceID, entityType, req.Key, req.Type, customer.AttributeDefinitionInput{
			Label: req.Label, Description: req.Description, Options: req.Options, AllowedSources: req.AllowedSources,
			RequiredCapability: req.RequiredCapability, Sensitive: req.Sensitive, Searchable: req.Searchable,
			Validation: req.Validation, RetentionDays: req.RetentionDays,
		})
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, attributeDefinitionJSON(*def))
	}
}

func handleUpdateAttributeDefinition(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req attributeDefinitionRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		def, err := deps.Customer.UpdateAttributeDefinition(r.Context(), actor.WorkspaceID, r.PathValue("id"), customer.AttributeDefinitionInput{
			Label: req.Label, Description: req.Description, Options: req.Options, AllowedSources: req.AllowedSources,
			RequiredCapability: req.RequiredCapability, Sensitive: req.Sensitive, Searchable: req.Searchable,
			Validation: req.Validation, RetentionDays: req.RetentionDays,
		})
		if err != nil {
			writeCustomerError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, attributeDefinitionJSON(*def))
	}
}

func handleArchiveAttributeDefinition(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Customer.ArchiveAttributeDefinition(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func attributeDefinitionJSON(d customer.AttributeDefinition) map[string]any {
	return map[string]any{
		"id": d.ID, "workspace_id": d.WorkspaceID, "entity_type": d.EntityType, "key": d.Key, "label": d.Label,
		"type": d.Type, "description": d.Description, "options": d.Options, "allowed_sources": d.AllowedSources,
		"required_capability": d.RequiredCapability, "sensitive": d.Sensitive, "searchable": d.Searchable,
		"validation": d.Validation, "retention_days": d.RetentionDays, "created_at": d.CreatedAt,
	}
}

type blockContactRequest struct {
	Kind   string  `json:"kind"`
	Value  string  `json:"value"`
	Reason *string `json:"reason"`
}

func handleBlockContact(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req blockContactRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Customer.Block(r.Context(), actor.WorkspaceID, actor.MemberID, req.Kind, req.Value, req.Reason); err != nil {
			writeCustomerError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeCustomerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, customer.ErrNotFound),
		errors.Is(err, customer.ErrCompanyNotFound),
		errors.Is(err, customer.ErrAttrNotFound),
		errors.Is(err, customer.ErrMergeNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Not found.")
	case errors.Is(err, customer.ErrVersionConflict):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, customer.ErrMergeAlreadyReversed), errors.Is(err, customer.ErrMergeWindowClosed):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, customer.ErrTagNotFound),
		errors.Is(err, customer.ErrInvalidBlockKind),
		errors.Is(err, customer.ErrInvalidCompanyName),
		errors.Is(err, customer.ErrCompanyExternalID),
		errors.Is(err, customer.ErrInvalidOwner),
		errors.Is(err, customer.ErrAttrInvalidEntity),
		errors.Is(err, customer.ErrAttrInvalidType),
		errors.Is(err, customer.ErrAttrInvalidKey),
		errors.Is(err, customer.ErrAttrDuplicateKey),
		errors.Is(err, customer.ErrAttrNotDeclared),
		errors.Is(err, customer.ErrAttrSourceNotAllowed),
		errors.Is(err, customer.ErrAttrBlockedKey),
		errors.Is(err, customer.ErrAttrInvalidValue),
		errors.Is(err, customer.ErrTooManyAttributes),
		errors.Is(err, customer.ErrEventTooLarge),
		errors.Is(err, customer.ErrInvalidSource),
		errors.Is(err, customer.ErrEmptyEventType),
		errors.Is(err, customer.ErrMergeSelf):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
