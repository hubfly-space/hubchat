package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/httpserver"
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

	// Starting a conversation has no natural idempotency key of its own — two
	// identical bodies are two legitimate conversations — so the header is the
	// only thing that can tell a retry from a second request.
	mux.HandleFunc("POST /v1/conversations",
		requireCapability(deps, authorization.ConversationReply,
			idempotent(handleStartConversation(deps))))

	mux.HandleFunc("GET /v1/conversations/{id}",
		requireCapability(deps, authorization.ConversationRead, handleGetConversation(deps)))

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
		requireCapability(deps, authorization.ConversationAssign, handleSetAssignee(deps)))
	mux.HandleFunc("PATCH /v1/conversations/{id}/team",
		requireCapability(deps, authorization.ConversationAssign, handleSetTeam(deps)))
	mux.HandleFunc("PATCH /v1/conversations/{id}/inbox",
		requireCapability(deps, authorization.ConversationAssign, handleSetInbox(deps)))
	mux.HandleFunc("PATCH /v1/conversations/{id}/priority",
		requireCapability(deps, authorization.ConversationAssign, handleSetPriority(deps)))
	mux.HandleFunc("PATCH /v1/conversations/{id}/state",
		requireCapability(deps, authorization.ConversationAssign, handleSetState(deps)))
	mux.HandleFunc("POST /v1/conversations/{id}/snooze",
		requireCapability(deps, authorization.ConversationAssign, handleSnooze(deps)))

	mux.HandleFunc("POST /v1/conversations/{id}/tags",
		requireCapability(deps, authorization.ConversationReply, handleAddConversationTag(deps)))
	mux.HandleFunc("DELETE /v1/conversations/{id}/tags/{tagID}",
		requireCapability(deps, authorization.ConversationReply, handleRemoveConversationTag(deps)))

	mux.HandleFunc("GET /v1/conversations/{id}/followers",
		requireCapability(deps, authorization.ConversationRead, handleListFollowers(deps)))
	mux.HandleFunc("PUT /v1/conversations/{id}/followers/me",
		requireCapability(deps, authorization.ConversationRead, handleFollow(deps)))
	mux.HandleFunc("DELETE /v1/conversations/{id}/followers/me",
		requireCapability(deps, authorization.ConversationRead, handleUnfollow(deps)))

	mux.HandleFunc("POST /v1/conversations/{id}/read",
		requireCapability(deps, authorization.ConversationRead, handleMarkRead(deps)))

	mux.HandleFunc("PATCH /v1/conversations/{id}/messages/{messageID}",
		requireCapability(deps, authorization.ConversationReply, handleEditMessage(deps)))
	mux.HandleFunc("POST /v1/conversations/{id}/messages/{messageID}/redact",
		requireCapability(deps, authorization.ConversationDelete, handleRedactMessage(deps)))
	mux.HandleFunc("POST /v1/conversations/{id}/merge",
		requireCapability(deps, authorization.ConversationAssign, handleMergeConversation(deps)))
	mux.HandleFunc("GET /v1/conversations/{id}/transcript",
		requireCapability(deps, authorization.ConversationRead, handleTranscript(deps)))
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
			"all":                counts.All,
			"unassigned":         counts.Unassigned,
			"mine":               counts.Mine,
			"following":          counts.Following,
			"waiting_on_us":      counts.WaitingOnUs,
			"waiting_on_customer": counts.WaitingOnCustomer,
			"snoozed":            counts.Snoozed,
			"resolved":           counts.Resolved,
			"spam":               counts.Spam,
		})
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
			Before:     cursor.At,
			BeforeID:   cursor.ID,
			// Queried at limit+1 so NewPage can tell "there is another page"
			// from one extra row rather than a second count query.
			Limit: limit + 1,
		}
		if state := query.Get("state"); state != "" {
			filter.States = strings.Split(state, ",")
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

		out := make([]map[string]any, len(page.Data))
		for i, c := range page.Data {
			out[i] = conversationJSON(c, tagsByConv[c.ID], !readByConv[c.ID], viewersFor(deps, c.ID))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{
			Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore,
		})
	}
}

type startConversationRequest struct {
	InboxID    string  `json:"inbox_id"`
	Channel    string  `json:"channel"`
	Subject    *string `json:"subject"`
	CustomerID *string `json:"customer_id"`
	AuthorName string  `json:"author_name"`
	Body       string  `json:"body"`
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
			req.Subject, req.CustomerID, req.AuthorName, req.Body,
		)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}

		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{
			"conversation": singleConversationJSON(r, deps, actor.WorkspaceID, actor.MemberID, *conv),
			"message":      messageJSON(*msg),
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

		var after int64
		if raw := r.URL.Query().Get("after"); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				after = parsed
			}
		}

		messages, err := deps.Conversation.Messages(r.Context(), actor.WorkspaceID, id, after)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}

		out := make([]any, len(messages))
		for i, m := range messages {
			out[i] = messageJSON(m)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type postMessageRequest struct {
	Kind       string `json:"kind"` // "reply" or "note"
	AuthorName string `json:"author_name"`
	Body       string `json:"body"`
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
		msg, err := deps.Conversation.PostMessage(
			r.Context(), actor.WorkspaceID, conversationID, clientID,
			kind, "agent", &memberID, authorName, req.Body,
		)
		if err != nil {
			writeConversationError(w, r, err)
			return
		}

		httpserver.WriteJSON(w, http.StatusCreated, messageJSON(*msg))
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

		followers, err := deps.Conversation.Followers(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeConversationError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": orEmpty(followers)})
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
		errors.Is(err, conversation.ErrInvalidPriority),
		errors.Is(err, conversation.ErrInvalidAssignee),
		errors.Is(err, conversation.ErrInvalidTeam),
		errors.Is(err, conversation.ErrInvalidInbox),
		errors.Is(err, conversation.ErrTagNotFound),
		errors.Is(err, conversation.ErrSnoozeInPast),
		errors.Is(err, conversation.ErrCannotMergeIntoSelf):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
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
	return conversationJSON(c, tagIDs, !read, viewersFor(deps, c.ID))
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
		// SLA tracking is Stage 8 (automation/sla module) — always null until
		// that module exists, per the shared contract's `ConversationSla | null`.
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
		// Attachments wait on internal/file (Stage 9) — always empty until
		// there is anywhere to upload one from.
		"attachments":       []string{},
		"quoted_message_id": m.QuotedMessageID,
		"delivery":          m.Delivery,
		"edited_at":         m.EditedAt,
		"redacted_at":       m.RedactedAt,
		"sequence":          m.Sequence,
		"created_at":        m.CreatedAt,
	}
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
