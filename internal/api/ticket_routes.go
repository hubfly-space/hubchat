package api

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/ticket"
)

func registerTicketRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/tickets",
		requireCapability(deps, authorization.TicketManage, handleListTickets(deps)))
	mux.HandleFunc("GET /v1/tickets/export",
		requireCapability(deps, authorization.TicketManage, handleExportTickets(deps)))
	mux.HandleFunc("GET /v1/tickets/duplicates",
		requireCapability(deps, authorization.TicketManage, handleDuplicateCandidates(deps)))
	mux.HandleFunc("POST /v1/tickets",
		requireCapability(deps, authorization.TicketManage, handleCreateTicket(deps)))
	mux.HandleFunc("GET /v1/tickets/{id}",
		requireCapability(deps, authorization.TicketManage, handleGetTicket(deps)))
	mux.HandleFunc("PATCH /v1/tickets/{id}",
		requireCapability(deps, authorization.TicketManage, handleUpdateTicketDetails(deps)))

	mux.HandleFunc("PATCH /v1/tickets/{id}/assignee",
		requireCapability(deps, authorization.TicketManage, handleSetTicketAssignee(deps)))
	mux.HandleFunc("PATCH /v1/tickets/{id}/team",
		requireCapability(deps, authorization.TicketManage, handleSetTicketTeam(deps)))
	mux.HandleFunc("PATCH /v1/tickets/{id}/inbox",
		requireCapability(deps, authorization.TicketManage, handleSetTicketInbox(deps)))
	mux.HandleFunc("PATCH /v1/tickets/{id}/priority",
		requireCapability(deps, authorization.TicketManage, handleSetTicketPriority(deps)))
	mux.HandleFunc("PATCH /v1/tickets/{id}/status",
		requireCapability(deps, authorization.TicketManage, handleSetTicketStatus(deps)))
	mux.HandleFunc("PATCH /v1/tickets/{id}/customer",
		requireCapability(deps, authorization.TicketManage, handleSetTicketCustomer(deps)))
	mux.HandleFunc("PATCH /v1/tickets/{id}/due",
		requireCapability(deps, authorization.TicketManage, handleSetTicketDueAt(deps)))
	mux.HandleFunc("PATCH /v1/tickets/{id}/parent",
		requireCapability(deps, authorization.TicketManage, handleSetTicketParent(deps)))
	mux.HandleFunc("GET /v1/tickets/{id}/children",
		requireCapability(deps, authorization.TicketManage, handleListTicketChildren(deps)))
	mux.HandleFunc("GET /v1/tickets/{id}/activity",
		requireCapability(deps, authorization.TicketManage, handleTicketActivity(deps)))

	mux.HandleFunc("POST /v1/tickets/{id}/tags",
		requireCapability(deps, authorization.TicketManage, handleAddTicketTag(deps)))
	mux.HandleFunc("DELETE /v1/tickets/{id}/tags/{tagID}",
		requireCapability(deps, authorization.TicketManage, handleRemoveTicketTag(deps)))

	mux.HandleFunc("GET /v1/tickets/{id}/followers",
		requireCapability(deps, authorization.TicketManage, handleListTicketFollowers(deps)))
	mux.HandleFunc("PUT /v1/tickets/{id}/followers/me",
		requireCapability(deps, authorization.TicketManage, handleFollowTicket(deps)))
	mux.HandleFunc("DELETE /v1/tickets/{id}/followers/me",
		requireCapability(deps, authorization.TicketManage, handleUnfollowTicket(deps)))

	mux.HandleFunc("GET /v1/tickets/{id}/links",
		requireCapability(deps, authorization.TicketManage, handleListTicketLinks(deps)))
	mux.HandleFunc("POST /v1/tickets/{id}/links",
		requireCapability(deps, authorization.TicketManage, handleAddTicketLink(deps)))
	mux.HandleFunc("DELETE /v1/tickets/{id}/links/{targetID}",
		requireCapability(deps, authorization.TicketManage, handleRemoveTicketLink(deps)))

	mux.HandleFunc("PUT /v1/tickets/{id}/field-values/{key}",
		requireCapability(deps, authorization.TicketManage, handleSetTicketFieldValue(deps)))

	mux.HandleFunc("GET /v1/field-definitions",
		requireCapability(deps, authorization.TicketManage, handleListFieldDefinitions(deps)))
	mux.HandleFunc("POST /v1/field-definitions",
		requireCapability(deps, authorization.TicketManage, handleCreateFieldDefinition(deps)))
	mux.HandleFunc("PATCH /v1/field-definitions/{id}",
		requireCapability(deps, authorization.TicketManage, handleUpdateFieldDefinition(deps)))
	mux.HandleFunc("DELETE /v1/field-definitions/{id}",
		requireCapability(deps, authorization.TicketManage, handleArchiveFieldDefinition(deps)))
	mux.HandleFunc("PUT /v1/field-definitions/reorder",
		requireCapability(deps, authorization.TicketManage, handleReorderFieldDefinitions(deps)))
}

func handleListTickets(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}

		filter := ticket.ListFilter{
			InboxID: query.Get("inbox_id"), AssigneeID: query.Get("assignee_id"),
			TeamID: query.Get("team_id"), CustomerID: query.Get("customer_id"),
			CompanyID: query.Get("company_id"), ParentID: query.Get("parent_id"),
			FollowerID: query.Get("follower_id"), TagID: query.Get("tag_id"), Priority: query.Get("priority"),
			Before: cursor.At, BeforeID: cursor.ID, Limit: limit + 1,
		}
		if status := query.Get("status"); status != "" {
			filter.Status = strings.Split(status, ",")
		}

		tickets, err := deps.Ticket.List(r.Context(), actor.WorkspaceID, filter)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load tickets.")
			return
		}

		page := NewPage(tickets, limit, func(t ticket.Ticket) Cursor {
			return Cursor{At: t.UpdatedAt, ID: t.ID}
		})

		ticketIDs := make([]string, len(page.Data))
		for i, t := range page.Data {
			ticketIDs[i] = t.ID
		}
		tagsByTicket, err := deps.Ticket.TagsForMany(r.Context(), actor.WorkspaceID, ticketIDs)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load tickets.")
			return
		}
		fieldsByTicket, err := deps.Ticket.FieldValuesForMany(r.Context(), actor.WorkspaceID, "ticket", ticketIDs)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load tickets.")
			return
		}

		out := make([]map[string]any, len(page.Data))
		for i, t := range page.Data {
			links, err := deps.Ticket.Links(r.Context(), actor.WorkspaceID, t.ID)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load tickets.")
				return
			}
			children, err := deps.Ticket.Children(r.Context(), actor.WorkspaceID, t.ID)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load tickets.")
				return
			}
			out[i] = ticketJSON(t, tagsByTicket[t.ID], fieldsByTicket[t.ID], children, linkedIDs(links, t.ID), ticketViewersFor(deps, t.ID))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleExportTickets(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		filter := ticket.ListFilter{
			InboxID: query.Get("inbox_id"), AssigneeID: query.Get("assignee_id"),
			TeamID: query.Get("team_id"), CustomerID: query.Get("customer_id"),
			Priority: query.Get("priority"), Limit: 10000,
		}
		if status := query.Get("status"); status != "" {
			filter.Status = strings.Split(status, ",")
		}

		tickets, err := deps.Ticket.List(r.Context(), actor.WorkspaceID, filter)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not export tickets.")
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="tickets-export.csv"`)
		w.WriteHeader(http.StatusOK)

		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"Number", "Title", "Status", "Priority", "Type", "Assignee ID", "Customer ID", "Company ID", "Created", "Due", "Resolved", "Closed"})
		for _, t := range tickets {
			_ = writer.Write([]string{
				fmt.Sprintf("%s-%d", t.Prefix, t.Number), t.Title, t.Status, t.Priority, derefOrEmpty(t.Type),
				derefOrEmpty(t.AssigneeID), derefOrEmpty(t.CustomerID), derefOrEmpty(t.CompanyID),
				t.CreatedAt.Format(timeFormat), formatTimePtr(t.DueAt), formatTimePtr(t.ResolvedAt), formatTimePtr(t.ClosedAt),
			})
		}
		writer.Flush()
	}
}

const timeFormat = "2006-01-02T15:04:05Z07:00"

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(timeFormat)
}

func handleDuplicateCandidates(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		title := query.Get("title")
		if title == "" {
			httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{}})
			return
		}
		var customerID, companyID *string
		if v := query.Get("customer_id"); v != "" {
			customerID = &v
		}
		if v := query.Get("company_id"); v != "" {
			companyID = &v
		}

		candidates, err := deps.Ticket.DuplicateCandidates(r.Context(), actor.WorkspaceID, query.Get("exclude_id"), title, customerID, companyID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not check for duplicates.")
			return
		}
		out := make([]map[string]any, len(candidates))
		for i, t := range candidates {
			out[i] = map[string]any{
				"id": t.ID, "number": t.Number, "prefix": t.Prefix, "title": t.Title, "status": t.Status,
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type createTicketRequest struct {
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Type           *string        `json:"type"`
	Priority       string         `json:"priority"`
	CustomerID     *string        `json:"customer_id"`
	CompanyID      *string        `json:"company_id"`
	InboxID        string         `json:"inbox_id"`
	Channel        string         `json:"channel"`
	AssigneeID     *string        `json:"assignee_id"`
	TeamID         *string        `json:"team_id"`
	ConversationID *string        `json:"conversation_id"`
	ParentID       *string        `json:"parent_id"`
	DueAt          *time.Time     `json:"due_at"`
	FieldValues    map[string]any `json:"field_values"`
}

func handleCreateTicket(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req createTicketRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		t, err := deps.Ticket.Create(r.Context(), actor.WorkspaceID, actor.MemberID, ticket.CreateRequest{
			Title: req.Title, Description: req.Description, Type: req.Type, Priority: req.Priority,
			CustomerID: req.CustomerID, CompanyID: req.CompanyID, InboxID: req.InboxID, Channel: req.Channel,
			AssigneeID: req.AssigneeID, TeamID: req.TeamID, ConversationID: req.ConversationID,
			ParentID: req.ParentID, DueAt: req.DueAt, FieldValues: req.FieldValues,
		})
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

func handleGetTicket(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		t, err := deps.Ticket.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type updateTicketDetailsRequest struct {
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Type            *string    `json:"type"`
	DueAt           *time.Time `json:"due_at"`
	ExpectedVersion int        `json:"expected_version"`
}

func handleUpdateTicketDetails(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req updateTicketDetailsRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		t, err := deps.Ticket.UpdateDetails(
			r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.ExpectedVersion,
			req.Title, req.Description, req.Type, req.DueAt,
		)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type setTicketAssigneeRequest struct {
	AssigneeID *string `json:"assignee_id"`
}

func handleSetTicketAssignee(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setTicketAssigneeRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		t, err := deps.Ticket.SetAssignee(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.AssigneeID)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type setTicketTeamRequest struct {
	TeamID *string `json:"team_id"`
}

func handleSetTicketTeam(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setTicketTeamRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		t, err := deps.Ticket.SetTeam(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TeamID)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type setTicketInboxRequest struct {
	InboxID string `json:"inbox_id"`
}

func handleSetTicketInbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setTicketInboxRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		t, err := deps.Ticket.SetInbox(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.InboxID)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type setTicketPriorityRequest struct {
	Priority string `json:"priority"`
}

func handleSetTicketPriority(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setTicketPriorityRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		t, err := deps.Ticket.SetPriority(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Priority)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type setTicketStatusRequest struct {
	Status string `json:"status"`
}

func handleSetTicketStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setTicketStatusRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		t, err := deps.Ticket.SetStatus(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Status)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type setTicketCustomerRequest struct {
	CustomerID *string `json:"customer_id"`
}

func handleSetTicketCustomer(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setTicketCustomerRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		t, err := deps.Ticket.SetCustomer(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.CustomerID)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type setTicketDueAtRequest struct {
	DueAt *time.Time `json:"due_at"`
}

func handleSetTicketDueAt(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setTicketDueAtRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		t, err := deps.Ticket.SetDueAt(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.DueAt)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

type setTicketParentRequest struct {
	ParentID *string `json:"parent_id"`
}

func handleSetTicketParent(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setTicketParentRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		t, err := deps.Ticket.SetParent(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.ParentID)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

// handleTicketActivity serves a single ticket's own history — reusing
// audit.Log directly rather than a ticket-specific event store, per Deps'
// own documented allowance for handlers to reach the shared audit reader for
// exactly this ("an entity's event timeline"). Gated on TicketManage rather
// than AuditRead: any agent who can see the ticket should see what happened
// to it, not just workspace admins.
func handleTicketActivity(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		records, err := deps.Audit.List(r.Context(), actor.WorkspaceID, audit.Filter{
			EntityType: "ticket", EntityID: r.PathValue("id"), Limit: 50,
		})
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load activity.")
			return
		}
		out := make([]auditLogJSON, len(records))
		for i, rec := range records {
			out[i] = auditRecordJSON(rec)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

func handleListTicketChildren(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		children, err := deps.Ticket.Children(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": orEmpty(children)})
	}
}

type addTicketTagRequest struct {
	TagID string `json:"tag_id"`
}

func handleAddTicketTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req addTicketTagRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if err := deps.Ticket.AddTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TagID); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveTicketTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Ticket.RemoveTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("tagID")); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListTicketFollowers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		followers, err := deps.Ticket.Followers(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": orEmpty(followers)})
	}
}

func handleFollowTicket(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Ticket.Follow(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleUnfollowTicket(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Ticket.Unfollow(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListTicketLinks(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		links, err := deps.Ticket.Links(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		out := make([]map[string]any, len(links))
		for i, l := range links {
			out[i] = map[string]any{"id": l.ID, "source_id": l.SourceID, "target_id": l.TargetID, "relation": l.Relation}
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type addTicketLinkRequest struct {
	TargetID string `json:"target_id"`
	Relation string `json:"relation"`
}

func handleAddTicketLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req addTicketLinkRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if err := deps.Ticket.Link(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TargetID, req.Relation); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveTicketLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		relation := r.URL.Query().Get("relation")
		if err := deps.Ticket.Unlink(r.Context(), actor.WorkspaceID, r.PathValue("id"), r.PathValue("targetID"), relation); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type setFieldValueRequest struct {
	Value any `json:"value"`
}

func handleSetTicketFieldValue(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req setFieldValueRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if err := deps.Ticket.SetFieldValue(r.Context(), actor.WorkspaceID, "ticket", r.PathValue("id"), r.PathValue("key"), req.Value); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------------------------------------------------------- field definitions

func handleListFieldDefinitions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		entityType := r.URL.Query().Get("entity_type")
		if entityType == "" {
			entityType = "ticket"
		}
		defs, err := deps.Ticket.ListFieldDefinitions(r.Context(), actor.WorkspaceID, entityType)
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		out := make([]map[string]any, len(defs))
		for i, d := range defs {
			out[i] = fieldDefinitionJSON(d)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type fieldDefinitionRequest struct {
	EntityType         string                  `json:"entity_type"`
	Key                string                  `json:"key"`
	Label              string                  `json:"label"`
	Type               string                  `json:"type"`
	Description        *string                 `json:"description"`
	Options            []string                `json:"options"`
	Required           bool                    `json:"required"`
	Visibility         string                  `json:"visibility"`
	Sensitive          bool                    `json:"sensitive"`
	Searchable         bool                    `json:"searchable"`
	AllowedSources     []string                `json:"allowed_sources"`
	RequiredCapability *string                 `json:"required_capability"`
	Validation         *ticket.FieldValidation `json:"validation"`
}

func handleCreateFieldDefinition(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req fieldDefinitionRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		entityType := req.EntityType
		if entityType == "" {
			entityType = "ticket"
		}
		def, err := deps.Ticket.CreateFieldDefinition(r.Context(), actor.WorkspaceID, entityType, req.Key, req.Type, ticket.FieldDefinitionInput{
			Label: req.Label, Description: req.Description, Options: req.Options, Required: req.Required,
			Visibility: req.Visibility, Sensitive: req.Sensitive, Searchable: req.Searchable,
			AllowedSources: req.AllowedSources, RequiredCapability: req.RequiredCapability, Validation: req.Validation,
		})
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, fieldDefinitionJSON(*def))
	}
}

func handleUpdateFieldDefinition(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req fieldDefinitionRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		def, err := deps.Ticket.UpdateFieldDefinition(r.Context(), actor.WorkspaceID, r.PathValue("id"), ticket.FieldDefinitionInput{
			Label: req.Label, Description: req.Description, Options: req.Options, Required: req.Required,
			Visibility: req.Visibility, Sensitive: req.Sensitive, Searchable: req.Searchable,
			AllowedSources: req.AllowedSources, RequiredCapability: req.RequiredCapability, Validation: req.Validation,
		})
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, fieldDefinitionJSON(*def))
	}
}

func handleArchiveFieldDefinition(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Ticket.ArchiveFieldDefinition(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type reorderFieldDefinitionsRequest struct {
	OrderedIDs []string `json:"ordered_ids"`
}

func handleReorderFieldDefinitions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req reorderFieldDefinitionsRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if err := deps.Ticket.ReorderFieldDefinitions(r.Context(), actor.WorkspaceID, req.OrderedIDs); err != nil {
			writeTicketError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func fieldDefinitionJSON(d ticket.FieldDefinition) map[string]any {
	return map[string]any{
		"id": d.ID, "workspace_id": d.WorkspaceID, "key": d.Key, "label": d.Label, "type": d.Type,
		"description": d.Description, "options": d.Options, "required": d.Required, "visibility": d.Visibility,
		"sensitive": d.Sensitive, "searchable": d.Searchable, "allowed_sources": d.AllowedSources,
		"required_capability": d.RequiredCapability, "validation": d.Validation, "created_at": d.CreatedAt,
	}
}

// ---------------------------------------------------------------- DTOs

func writeTicketError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ticket.ErrNotFound), errors.Is(err, ticket.ErrFieldNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Not found.")
	case errors.Is(err, ticket.ErrVersionConflict):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, ticket.ErrEmptyTitle),
		errors.Is(err, ticket.ErrInvalidStatus),
		errors.Is(err, ticket.ErrInvalidPriority),
		errors.Is(err, ticket.ErrInvalidAssignee),
		errors.Is(err, ticket.ErrInvalidTeam),
		errors.Is(err, ticket.ErrInvalidInbox),
		errors.Is(err, ticket.ErrInvalidCustomer),
		errors.Is(err, ticket.ErrInvalidCompany),
		errors.Is(err, ticket.ErrTagNotFound),
		errors.Is(err, ticket.ErrInvalidRelation),
		errors.Is(err, ticket.ErrLinkToSelf),
		errors.Is(err, ticket.ErrInvalidParent),
		errors.Is(err, ticket.ErrParentCycle),
		errors.Is(err, ticket.ErrParentIsSelf),
		errors.Is(err, ticket.ErrInvalidEntityType),
		errors.Is(err, ticket.ErrInvalidFieldType),
		errors.Is(err, ticket.ErrInvalidVisibility),
		errors.Is(err, ticket.ErrInvalidKey),
		errors.Is(err, ticket.ErrFieldRequired),
		errors.Is(err, ticket.ErrInvalidFieldValue),
		errors.Is(err, ticket.ErrDuplicateKey):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}

func singleTicketJSON(r *http.Request, deps Deps, workspaceID string, t ticket.Ticket) map[string]any {
	tagIDs, err := deps.Ticket.Tags(r.Context(), workspaceID, t.ID)
	if err != nil {
		tagIDs = []string{}
	}
	fieldValues, err := deps.Ticket.FieldValues(r.Context(), workspaceID, "ticket", t.ID)
	if err != nil {
		fieldValues = map[string]any{}
	}
	children, err := deps.Ticket.Children(r.Context(), workspaceID, t.ID)
	if err != nil {
		children = []string{}
	}
	links, err := deps.Ticket.Links(r.Context(), workspaceID, t.ID)
	if err != nil {
		links = []ticket.TicketLink{}
	}
	return ticketJSON(t, tagIDs, fieldValues, children, linkedIDs(links, t.ID), ticketViewersFor(deps, t.ID))
}

// linkedIDs flattens ticket_links into the "other ticket" id regardless of
// which side of the relation this ticket is on — the shared contract's
// linked_ticket_ids is a flat list; relation detail is exposed separately by
// GET .../links for the UI that needs to label it.
func linkedIDs(links []ticket.TicketLink, ticketID string) []string {
	out := make([]string, 0, len(links))
	for _, l := range links {
		if l.SourceID == ticketID {
			out = append(out, l.TargetID)
		} else {
			out = append(out, l.SourceID)
		}
	}
	return out
}

func ticketViewersFor(deps Deps, ticketID string) []string {
	if deps.Hub == nil {
		return []string{}
	}
	return orEmpty(deps.Hub.TicketViewers(ticketID))
}

func ticketJSON(t ticket.Ticket, tagIDs []string, fieldValues map[string]any, childIDs, linkedTicketIDs, viewers []string) map[string]any {
	if fieldValues == nil {
		fieldValues = map[string]any{}
	}
	return map[string]any{
		"id": t.ID, "workspace_id": t.WorkspaceID, "number": t.Number, "prefix": t.Prefix,
		"title": t.Title, "description": t.Description, "status": t.Status, "priority": t.Priority,
		"type": t.Type, "customer_id": t.CustomerID, "company_id": t.CompanyID,
		"inbox_id": derefOrEmpty(t.InboxID), "channel": t.Channel,
		"assignee_id": t.AssigneeID, "team_id": t.TeamID, "conversation_id": t.ConversationID,
		"parent_id": t.ParentID, "child_ids": orEmpty(childIDs), "linked_ticket_ids": orEmpty(linkedTicketIDs),
		"tag_ids": orEmpty(tagIDs), "field_values": fieldValues,
		// SLA tracking is Stage 8 (automation/sla module) — always null until
		// that module exists, per the shared contract's `ConversationSla | null`.
		"sla": nil, "viewers": viewers,
		"due_at": t.DueAt, "version": t.Version, "created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
		"resolved_at": t.ResolvedAt, "closed_at": t.ClosedAt,
	}
}

func derefOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
