package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerConversationRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/inboxes/{inboxID}/conversations",
		requireCapability(deps, authorization.ConversationRead, handleListConversations(deps)))

	mux.HandleFunc("POST /v1/conversations",
		requireCapability(deps, authorization.ConversationReply, handleStartConversation(deps)))

	mux.HandleFunc("GET /v1/conversations/{id}",
		requireCapability(deps, authorization.ConversationRead, handleGetConversation(deps)))

	mux.HandleFunc("GET /v1/conversations/{id}/messages",
		requireCapability(deps, authorization.ConversationRead, handleListMessages(deps)))

	// A message is either a public reply or an internal note; both go through
	// the same endpoint and are distinguished by "kind" in the body, matching
	// the composer's own reply/note toggle (§6.2).
	mux.HandleFunc("POST /v1/conversations/{id}/messages",
		requireCapability(deps, authorization.ConversationReply, handlePostMessage(deps)))
}

func handleListConversations(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		inboxID := r.PathValue("inboxID")

		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil {
				limit = parsed
			}
		}

		conversations, err := deps.Conversation.ListActive(r.Context(), actor.WorkspaceID, inboxID, limit)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load conversations.")
			return
		}

		out := make([]any, len(conversations))
		for i, c := range conversations {
			out[i] = conversationJSON(c)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
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
			"conversation": conversationJSON(*conv),
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
		httpserver.WriteJSON(w, http.StatusOK, conversationJSON(*conv))
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

func writeConversationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, conversation.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Conversation not found.")
	case errors.Is(err, conversation.ErrEmptyBody):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "Message body must not be empty.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}

func conversationJSON(c conversation.Conversation) map[string]any {
	return map[string]any{
		"id":                    c.ID,
		"workspace_id":          c.WorkspaceID,
		"inbox_id":              c.InboxID,
		"channel":               c.Channel,
		"subject":               c.Subject,
		"state":                 c.State,
		"priority":              c.Priority,
		"customer_id":           c.CustomerID,
		"assignee_id":           c.AssigneeID,
		"team_id":               c.TeamID,
		"message_count":         c.MessageCount,
		"last_message_preview":  c.LastMessagePreview,
		"last_message_at":       c.LastMessageAt,
		"created_at":            c.CreatedAt,
	}
}

func messageJSON(m conversation.Message) map[string]any {
	return map[string]any{
		"id":              m.ID,
		"client_id":       m.ClientID,
		"conversation_id": m.ConversationID,
		"kind":            m.Kind,
		"author_type":     m.AuthorType,
		"author_id":       m.AuthorID,
		"author_name":     m.AuthorName,
		"body":            m.Body,
		"sequence":        m.Sequence,
		"created_at":      m.CreatedAt,
	}
}
