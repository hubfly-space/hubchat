package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/sla"
	"github.com/hubchat/hubchat/internal/ticket"
)

func registerConversationRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)

	// Workspace-scoped, not inbox-scoped: "assigned to me" and "unassigned"
	// cross every inbox the viewer can see, so inbox_id is an optional filter
	// (see conversation.ListFilter) rather than a path segment.
	mux.HandleFunc("GET /v1/conversations",
		requireCapability(deps, authorization.ConversationRead, handleListConversations(deps)))
	mux.HandleFunc("GET /v1/conversations/counts",
		requireCapability(deps, authorization.ConversationRead, handleConversationCounts(deps)))
	mux.HandleFunc("POST /v1/conversations/bulk",
		requireCapability(deps, authorization.ConversationAssign,
			idempotent(handleBulkConversationUpdate(deps))))

	// Starting a conversation has no natural idempotency key of its own — two
	// identical bodies are two legitimate conversations — so the header is the
	// only thing that can tell a retry from a second request.
	mux.HandleFunc("POST /v1/conversations",
		requireCapability(deps, authorization.ConversationReply,
			idempotent(handleStartConversation(deps))))
	mux.HandleFunc("POST /v1/conversations/{id}/ticket",
		requireCapability(deps, authorization.TicketManage,
			idempotent(handleConvertConversationToTicket(deps))))

	mux.HandleFunc("GET /v1/conversations/{id}",
		requireCapability(deps, authorization.ConversationRead, handleGetConversation(deps)))
	mux.HandleFunc("GET /v1/conversations/{id}/links",
		requireCapability(deps, authorization.ConversationRead, handleListConversationLinks(deps)))
	mux.HandleFunc("POST /v1/conversations/{id}/links",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleLinkConversation(deps))))
	mux.HandleFunc("DELETE /v1/conversations/{id}/links/{targetID}",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleUnlinkConversation(deps))))

	mux.HandleFunc("GET /v1/conversations/{id}/messages",
		requireCapability(deps, authorization.ConversationRead, handleListMessages(deps)))

	// A message is either a public reply or an internal note; both go through
	// the same endpoint and are distinguished by "kind" in the body, matching
	// the composer's own reply/note toggle (§6.2).
	//
	// Messages have a second, stronger guard: the client_id column, enforced by
	// a unique index. The header layer catches a retry before the handler runs;
	// the column catches one that races past it. Both exist because a duplicated
	// customer reply is visible and embarrassing in a way a duplicated read is
	// not.
	mux.HandleFunc("POST /v1/conversations/{id}/messages",
		requireCapability(deps, authorization.ConversationReply,
			idempotent(handlePostMessage(deps))))

	mux.HandleFunc("PATCH /v1/conversations/{id}/assignee",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleSetAssignee(deps))))
	mux.HandleFunc("PATCH /v1/conversations/{id}/team",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleSetTeam(deps))))
	mux.HandleFunc("PATCH /v1/conversations/{id}/inbox",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleSetInbox(deps))))
	mux.HandleFunc("PATCH /v1/conversations/{id}/priority",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleSetPriority(deps))))
	mux.HandleFunc("PATCH /v1/conversations/{id}/state",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleSetState(deps))))
	mux.HandleFunc("POST /v1/conversations/{id}/snooze",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleSnooze(deps))))

	mux.HandleFunc("POST /v1/conversations/{id}/tags",
		requireCapability(deps, authorization.ConversationReply, idempotent(handleAddConversationTag(deps))))
	mux.HandleFunc("DELETE /v1/conversations/{id}/tags/{tagID}",
		requireCapability(deps, authorization.ConversationReply, idempotent(handleRemoveConversationTag(deps))))

	mux.HandleFunc("GET /v1/conversations/{id}/followers",
		requireCapability(deps, authorization.ConversationRead, handleListFollowers(deps)))
	mux.HandleFunc("PUT /v1/conversations/{id}/followers/me",
		requireCapability(deps, authorization.ConversationRead, idempotent(handleFollow(deps))))
	mux.HandleFunc("DELETE /v1/conversations/{id}/followers/me",
		requireCapability(deps, authorization.ConversationRead, idempotent(handleUnfollow(deps))))

	mux.HandleFunc("POST /v1/conversations/{id}/read",
		requireCapability(deps, authorization.ConversationRead, idempotent(handleMarkRead(deps))))

	mux.HandleFunc("PATCH /v1/conversations/{id}/messages/{messageID}",
		requireCapability(deps, authorization.ConversationReply, idempotent(handleEditMessage(deps))))
	mux.HandleFunc("POST /v1/conversations/{id}/messages/{messageID}/redact",
		requireCapability(deps, authorization.ConversationDelete, idempotent(handleRedactMessage(deps))))
	mux.HandleFunc("POST /v1/conversations/{id}/merge",
		requireCapability(deps, authorization.ConversationAssign, idempotent(handleMergeConversation(deps))))
	mux.HandleFunc("GET /v1/conversations/{id}/transcript",
		requireCapability(deps, authorization.ConversationRead, handleTranscript(deps)))
}

// handleConvertConversationToTicket turns the current conversation into a
// tracked ticket while preserving the original thread. Ticket.Create owns the
// transaction and writes the reverse conversation.ticket_id link atomically.
func handleConvertConversationToTicket(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req createTicketRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		conv, err := deps.Conversation.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" && conv.Subject != nil {
			title = strings.TrimSpace(*conv.Subject)
		}
		if title == "" {
			title = "Support request"
		}
		inboxID := req.InboxID
		if inboxID == "" {
			inboxID = conv.InboxID
		}
		customerID := req.CustomerID
		if customerID == nil {
			customerID = conv.CustomerID
		}
		channel := req.Channel
		if channel == "" {
			channel = conv.Channel
		}
		conversationID := conv.ID
		t, err := deps.Ticket.Create(r.Context(), actor.WorkspaceID, actor.MemberID, ticket.CreateRequest{
			Title: title, Description: req.Description, Type: req.Type, Priority: req.Priority,
			CustomerID: customerID, CompanyID: req.CompanyID, InboxID: inboxID, Channel: channel,
			AssigneeID: req.AssigneeID, TeamID: req.TeamID, ConversationID: &conversationID,
			ParentID: req.ParentID, DueAt: req.DueAt, FieldValues: req.FieldValues,
		})
		if err != nil {
			writeTicketError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, singleTicketJSON(r, deps, actor.WorkspaceID, *t))
	}
}

func handleConversationCounts(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		counts, err := deps.Conversation.Counts(r.Context(), actor.WorkspaceID, actor.MemberID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load counts.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"all":                 counts.All,
			"unassigned":          counts.Unassigned,
			"mine":                counts.Mine,
			"following":           counts.Following,
			"mentioned":           counts.Mentioned,
			"waiting_on_us":       counts.WaitingOnUs,
			"waiting_on_customer": counts.WaitingOnCustomer,
			"snoozed":             counts.Snoozed,
			"resolved":            counts.Resolved,
			"spam":                counts.Spam,
			"sla_approaching":     counts.SLAApproaching,
			"sla_breached":        counts.SLABreached,
		})
	}
}

type bulkConversationUpdateRequest struct {
	IDs        []string `json:"ids"`
	Action     string   `json:"action"`
	AssigneeID *string  `json:"assignee_id"`
	State      string   `json:"state"`
}

// handleBulkConversationUpdate deliberately exposes only the two operations
// used by the inbox selection toolbar. Keeping the action vocabulary narrow
// prevents a generic bulk endpoint from becoming an authorization bypass for
// mutations that have different invariants (snooze, merge, or ticket links).
func handleBulkConversationUpdate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req bulkConversationUpdateRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		items, err := deps.Conversation.BulkUpdate(r.Context(), actor.WorkspaceID, actor.MemberID, req.IDs, req.Action, req.AssigneeID, req.State)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		updated := make([]map[string]any, 0, len(items))
		for _, item := range items {
			updated = append(updated, map[string]any{
				"id": item.ID, "state": item.State, "assignee_id": item.AssigneeID,
			})
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": updated, "count": len(updated)})
	}
}

func handleListConversations(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		query := r.URL.Query()

		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}

		filter := conversation.ListFilter{
			InboxID:    query.Get("inbox_id"),
			AssigneeID: query.Get("assignee_id"),
			TeamID:     query.Get("team_id"),
			Priority:   query.Get("priority"),
			TagID:      query.Get("tag_id"),
			FollowerID: query.Get("follower_id"),
			CustomerID: query.Get("customer_id"),
			SLAState:   query.Get("sla"),
			Before:     cursor.At,
			BeforeID:   cursor.ID,
			// Queried at limit+1 so NewPage can tell "there is another page"
			// from one extra row rather than a second count query.
			Limit: limit + 1,
		}
		if state := query.Get("state"); state != "" {
			filter.States = strings.Split(state, ",")
		}
		if filter.SLAState != "" && filter.SLAState != "approaching" && filter.SLAState != "breached" {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "sla must be approaching or breached.")
			return
		}
		if raw := query.Get("mentioned"); raw != "" {
			mentioned, parseErr := strconv.ParseBool(raw)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "mentioned must be true or false.")
				return
			}
			if mentioned {
				filter.MentionedMemberID = actor.MemberID
			}
		}

		conversations, err := deps.Conversation.List(r.Context(), actor.WorkspaceID, filter)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load conversations.")
			return
		}

		page := NewPage(conversations, limit, func(c conversation.Conversation) Cursor {
			return Cursor{At: c.LastMessageAt, ID: c.ID}
		})

		ids := make([]string, len(page.Data))
		for i, c := range page.Data {
			ids[i] = c.ID
		}
		tagsByConv, err := deps.Conversation.TagsForMany(r.Context(), actor.WorkspaceID, ids)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load conversations.")
			return
		}
		readByConv, err := deps.Conversation.IsReadForMany(r.Context(), actor.WorkspaceID, ids, actor.MemberID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load conversations.")
			return
		}
		var slaByConv map[string]*sla.SubjectSLA
		if deps.SLA != nil {
			slaByConv, err = deps.SLA.ConversationSLAs(r.Context(), actor.WorkspaceID, ids)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load conversation SLA.")
				return
			}
		}

		out := make([]map[string]any, len(page.Data))
		for i, c := range page.Data {
			out[i] = conversationJSON(c, tagsByConv[c.ID], !readByConv[c.ID], viewersFor(deps, c.ID))
			if deps.SLA != nil {
				out[i]["sla"] = slaJSON(slaByConv[c.ID])
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{
			Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore,
		})
	}
}

type startConversationRequest struct {
	InboxID    string   `json:"inbox_id"`
	Channel    string   `json:"channel"`
	Subject    *string  `json:"subject"`
	CustomerID *string  `json:"customer_id"`
	AuthorName string   `json:"author_name"`
	Body       string   `json:"body"`
	FileIDs    []string `json:"file_ids"`
}

func handleStartConversation(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req startConversationRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if req.InboxID == "" || req.Channel == "" {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError,
				"inbox_id and channel are required.")
			return
		}

		conv, msg, err := deps.Conversation.Start(
			r.Context(), actor.WorkspaceID, req.InboxID, req.Channel,
			req.Subject, req.CustomerID, nil, req.AuthorName, req.Body,
		)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		if deps.File != nil && len(req.FileIDs) > 0 {
			if err := deps.File.AttachToMessage(r.Context(), actor.WorkspaceID, msg.ID, req.FileIDs); err != nil {
				writeMessageAttachmentError(w, r, err)
				return
			}
		}

		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{
			"conversation": singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv),
			"message":      messageJSONWithAttachments(r, deps, actor.WorkspaceID, *msg),
		})
	}
}

func handleGetConversation(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		id := r.PathValue("id")

		conv, err := deps.Conversation.Get(r.Context(), actor.WorkspaceID, id)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv))
	}
}

func handleListMessages(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		id := r.PathValue("id")

		before, after, limit, err := messagePageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, err.Error())
			return
		}

		messages, hasMore, err := deps.Conversation.ListMessagesPage(r.Context(), actor.WorkspaceID, id, before, after, limit)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}

		out := make([]any, len(messages))
		for i, m := range messages {
			out[i] = messageJSONWithAttachments(r, deps, actor.WorkspaceID, m)
		}
		response := map[string]any{"data": out, "has_more": hasMore, "next_cursor": nil}
		if hasMore && len(messages) > 0 && after == 0 {
			response["next_cursor"] = Cursor{Value: strconv.FormatInt(messages[0].Sequence, 10)}.Encode()
		}
		if hasMore && len(messages) > 0 && after > 0 {
			response["next_after"] = messages[len(messages)-1].Sequence
		}
		httpserver.WriteJSON(w, http.StatusOK, response)
	}
}

func messagePageParams(r *http.Request) (before, after int64, limit int, err error) {
	limit = 100
	query := r.URL.Query()
	beforeRaw := query.Get("before")
	if encoded := query.Get("cursor"); encoded != "" {
		cursor, decodeErr := DecodeCursor(encoded)
		if decodeErr != nil || cursor.Value == "" {
			return 0, 0, 0, errors.New("cursor is malformed")
		}
		beforeRaw = cursor.Value
	}
	parse := func(name, raw string) (int64, error) {
		if raw == "" {
			return 0, nil
		}
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 1 {
			return 0, fmt.Errorf("%s must be a positive integer", name)
		}
		return value, nil
	}
	if before, err = parse("before", beforeRaw); err != nil {
		return 0, 0, 0, err
	}
	if after, err = parse("after", query.Get("after")); err != nil {
		return 0, 0, 0, err
	}
	if before > 0 && after > 0 {
		return 0, 0, 0, errors.New("before and after cannot be used together")
	}
	if raw := query.Get("limit"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 200 {
			return 0, 0, 0, errors.New("limit must be between 1 and 200")
		}
		limit = parsed
	}
	return before, after, limit, nil
}

type postMessageRequest struct {
	Kind               string   `json:"kind"` // "reply" or "note"
	AuthorName         string   `json:"author_name"`
	Body               string   `json:"body"`
	FileIDs            []string `json:"file_ids"`
	MentionedMemberIDs []string `json:"mentioned_member_ids"`
}

func handlePostMessage(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		conversationID := r.PathValue("id")

		var req postMessageRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		kind := req.Kind
		if kind == "" {
			kind = "reply"
		}
		if kind != "reply" && kind != "note" {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError,
				`kind must be "reply" or "note".`)
			return
		}

		authorName := strings.TrimSpace(req.AuthorName)
		if authorName == "" {
			authorName = "Agent"
		}

		var clientID *string
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			clientID = &key
		}

		memberID := actor.MemberID
		msg, err := deps.Conversation.PostMessageWithMentions(
			r.Context(), actor.WorkspaceID, conversationID, clientID,
			kind, "agent", &memberID, authorName, req.Body, req.MentionedMemberIDs,
		)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		if deps.File != nil && len(req.FileIDs) > 0 {
			if err := deps.File.AttachToMessage(r.Context(), actor.WorkspaceID, msg.ID, req.FileIDs); err != nil {
				writeMessageAttachmentError(w, r, err)
				return
			}
		}
		if deps.Notification != nil {
			if notifyErr := deps.Notification.NotifyConversationMessage(r.Context(), actor.WorkspaceID, conversationID,
				msg.ID, msg.AuthorType, actor.MemberID, msg.Body); notifyErr != nil && deps.Logger != nil {
				deps.Logger.Warn("could not create conversation notification", "conversation_id", conversationID, "error", notifyErr)
			}
		}

		httpserver.WriteJSON(w, http.StatusCreated, messageJSONWithAttachments(r, deps, actor.WorkspaceID, *msg))
	}
}

type setAssigneeRequest struct {
	AssigneeID *string `json:"assignee_id"`
}

func handleSetAssignee(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setAssigneeRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		conv, err := deps.Conversation.SetAssignee(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.AssigneeID)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv))
	}
}

type setTeamRequest struct {
	TeamID *string `json:"team_id"`
}

func handleSetTeam(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setTeamRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		conv, err := deps.Conversation.SetTeam(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TeamID)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv))
	}
}

type setInboxRequest struct {
	InboxID string `json:"inbox_id"`
}

func handleSetInbox(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setInboxRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		conv, err := deps.Conversation.SetInbox(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.InboxID)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv))
	}
}

type setPriorityRequest struct {
	Priority string `json:"priority"`
}

func handleSetPriority(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setPriorityRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		conv, err := deps.Conversation.SetPriority(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Priority)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv))
	}
}

type setStateRequest struct {
	State string `json:"state"`
}

func handleSetState(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req setStateRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		conv, err := deps.Conversation.SetState(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.State)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv))
	}
}

type snoozeRequest struct {
	Until time.Time `json:"until"`
}

func handleSnooze(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req snoozeRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		conv, err := deps.Conversation.Snooze(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Until)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv))
	}
}

type addTagRequest struct {
	TagID string `json:"tag_id"`
}

func handleAddConversationTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req addTagRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Conversation.AddTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TagID); err != nil {
			writeConversationError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRemoveConversationTag(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Conversation.RemoveTag(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("tagID")); err != nil {
			writeConversationError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListFollowers(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}

		followers, err := deps.Conversation.FollowersPage(r.Context(), actor.WorkspaceID, r.PathValue("id"), cursor.Value, limit+1)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(followers, limit, func(memberID string) Cursor { return Cursor{Value: memberID} }))
	}
}

func handleFollow(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Conversation.Follow(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID); err != nil {
			writeConversationError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleUnfollow(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Conversation.Unfollow(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID); err != nil {
			writeConversationError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMarkRead(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		if err := deps.Conversation.MarkRead(r.Context(), actor.WorkspaceID, r.PathValue("id"), actor.MemberID); err != nil {
			writeConversationError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeConversationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, conversation.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Conversation not found.")
	case errors.Is(err, conversation.ErrMessageNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Message not found.")
	case errors.Is(err, conversation.ErrEmptyBody):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "Message body must not be empty.")
	case errors.Is(err, conversation.ErrNotMessageAuthor):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, err.Error())
	case errors.Is(err, conversation.ErrInvalidState),
		errors.Is(err, conversation.ErrInvalidBulkAction),
		errors.Is(err, conversation.ErrBulkTooLarge),
		errors.Is(err, conversation.ErrInvalidPriority),
		errors.Is(err, conversation.ErrInvalidAssignee),
		errors.Is(err, conversation.ErrInvalidTeam),
		errors.Is(err, conversation.ErrInvalidInbox),
		errors.Is(err, conversation.ErrInvalidMention),
		errors.Is(err, conversation.ErrTagNotFound),
		errors.Is(err, conversation.ErrSnoozeInPast),
		errors.Is(err, conversation.ErrCannotMergeIntoSelf),
		errors.Is(err, conversation.ErrInvalidLinkRelation),
		errors.Is(err, conversation.ErrLinkToSelf):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, conversation.ErrLinkAlreadyExists):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "That conversation link already exists.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}

// singleConversationJSON builds the DTO for one conversation, fetching its
// tags and read state directly. Handlers touching exactly one conversation
// (start, get, and every mutation that returns the row it changed) use this;
// the list endpoint batches both lookups across the page instead (see
// handleListConversations) to avoid two extra round trips per row.
func singleConversationJSON(r *http.Request, deps Deps, workspaceID, memberID string, c conversation.Conversation) map[string]any {
	tagIDs, err := deps.Conversation.Tags(r.Context(), workspaceID, c.ID)
	if err != nil {
		tagIDs = []string{}
	}
	read, err := deps.Conversation.IsRead(r.Context(), workspaceID, c.ID, memberID)
	if err != nil {
		read = true
	}
	out := conversationJSON(c, tagIDs, !read, viewersFor(deps, c.ID))
	if deps.SLA != nil {
		if item, err := deps.SLA.ConversationSLA(r.Context(), workspaceID, c.ID); err == nil {
			out["sla"] = slaJSON(item)
		} else if deps.Logger != nil {
			deps.Logger.Warn("could not load conversation SLA", "conversation_id", c.ID, "error", err)
		}
	}
	return out
}

// viewersFor is an in-memory hub lookup, not a database query — cheap enough
// to call once per row in the list handler too, unlike Tags/IsRead which
// batch across a page for exactly that reason.
func viewersFor(deps Deps, conversationID string) []string {
	if deps.Hub == nil {
		return []string{}
	}
	return orEmpty(deps.Hub.Viewers(conversationID))
}

func conversationJSON(c conversation.Conversation, tagIDs []string, unread bool, viewers []string) map[string]any {
	return map[string]any{
		"id":                   c.ID,
		"workspace_id":         c.WorkspaceID,
		"inbox_id":             c.InboxID,
		"channel":              c.Channel,
		"subject":              c.Subject,
		"state":                c.State,
		"priority":             c.Priority,
		"customer_id":          c.CustomerID,
		"visitor_id":           c.VisitorID,
		"assignee_id":          c.AssigneeID,
		"team_id":              c.TeamID,
		"tag_ids":              orEmpty(tagIDs),
		"ticket_id":            c.TicketID,
		"unread":               unread,
		"message_count":        c.MessageCount,
		"last_message_preview": c.LastMessagePreview,
		"last_message_at":      c.LastMessageAt,
		"last_customer_at":     c.LastCustomerAt,
		"snoozed_until":        c.SnoozedUntil,
		// The handler attaches the live SLA summary after the base DTO is built.
		"sla":        nil,
		"viewers":    viewers,
		"created_at": c.CreatedAt,
	}
}

func messageJSON(m conversation.Message) map[string]any {
	return map[string]any{
		"id":                m.ID,
		"client_id":         m.ClientID,
		"conversation_id":   m.ConversationID,
		"kind":              m.Kind,
		"author_type":       m.AuthorType,
		"author_id":         m.AuthorID,
		"author_name":       m.AuthorName,
		"author_avatar_url": nil,
		"body":              m.Body,
		"event_type":        nil,
		"event_data":        nil,
		"attachments":       []string{},
		"quoted_message_id": m.QuotedMessageID,
		"delivery":          m.Delivery,
		"edited_at":         m.EditedAt,
		"redacted_at":       m.RedactedAt,
		"sequence":          m.Sequence,
		"created_at":        m.CreatedAt,
	}
}

func messageJSONWithAttachments(r *http.Request, deps Deps, workspaceID string, m conversation.Message) map[string]any {
	result := messageJSON(m)
	if deps.File == nil {
		return result
	}
	attachments, err := deps.File.MessageAttachments(r.Context(), workspaceID, m.ID)
	if err != nil {
		if deps.Logger != nil {
			deps.Logger.Warn("could not load message attachments", "message_id", m.ID, "error", err)
		}
		return result
	}
	out := make([]any, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, fileJSON(attachment))
	}
	result["attachments"] = out
	return result
}

func writeMessageAttachmentError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, file.ErrInvalidAttachment) {
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError,
			"One or more attachments are not available in this workspace.")
		return
	}
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError,
		"The message was sent, but its attachments could not be linked.")
}

type editMessageRequest struct {
	Body string `json:"body"`
}

func handleEditMessage(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req editMessageRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		msg, err := deps.Conversation.EditMessage(
			r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("messageID"), req.Body,
		)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, messageJSON(*msg))
	}
}

func handleRedactMessage(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		msg, err := deps.Conversation.RedactMessage(
			r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("messageID"),
		)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, messageJSON(*msg))
	}
}

type mergeConversationRequest struct {
	TargetID string `json:"target_id"`
}

func handleMergeConversation(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		var req mergeConversationRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Conversation.Merge(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TargetID); err != nil {
			writeConversationError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListConversationLinks(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed conversation link cursor.")
			return
		}
		links, err := deps.Conversation.LinksPage(r.Context(), actor.WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		page := NewPage(links, limit, func(link conversation.ConversationLink) Cursor {
			return Cursor{At: link.CreatedAt, ID: link.ID}
		})
		out := make([]map[string]any, 0, len(page.Data))
		for _, link := range page.Data {
			out = append(out, conversationLinkJSON(link))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

type conversationLinkRequest struct {
	TargetID string `json:"target_id"`
	Relation string `json:"relation"`
}

func handleLinkConversation(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req conversationLinkRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.TargetID) == "" {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "A target conversation is required.")
			return
		}
		if strings.TrimSpace(req.Relation) == "" {
			req.Relation = "related"
		}
		link, err := deps.Conversation.Link(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.TargetID, req.Relation)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, conversationLinkJSON(*link))
	}
}

func handleUnlinkConversation(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		relation := r.URL.Query().Get("relation")
		if relation == "" {
			relation = "related"
		}
		if err := deps.Conversation.Unlink(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("targetID"), relation); err != nil {
			writeConversationError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func conversationLinkJSON(link conversation.ConversationLink) map[string]any {
	return map[string]any{
		"id": link.ID, "workspace_id": link.WorkspaceID, "source_id": link.SourceID,
		"target_id": link.TargetID, "relation": link.Relation, "created_by": link.CreatedBy,
		"created_at": link.CreatedAt,
	}
}

func handleTranscript(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)

		text, err := deps.Conversation.Transcript(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("id")+`-transcript.txt"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(text))
	}
}
