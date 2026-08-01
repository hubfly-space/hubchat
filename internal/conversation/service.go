// Package conversation owns threads, messages, state transitions, and
// assignment.
//
// # Responsibilities
//
// The operational core: create, reply, note, assign, snooze, merge, split,
// resolve. Owns the per-conversation sequence counter that realtime resume
// depends on.
//
// # Boundary
//
// State transitions are validated here, not in handlers — an invalid
// transition must be impossible via any entry point, including the API and
// automation.
package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrEmptyBody       = errors.New("conversation: message body must not be empty")
	ErrInvalidState    = errors.New("conversation: not a recognised state")
	ErrInvalidPriority = errors.New("conversation: not a recognised priority")
	ErrInvalidAssignee = errors.New("conversation: assignee is not a member of this workspace")
	ErrInvalidTeam     = errors.New("conversation: not a team in this workspace")
	ErrInvalidInbox    = errors.New("conversation: not an inbox in this workspace")
	ErrTagNotFound     = errors.New("conversation: not a tag in this workspace")
	ErrSnoozeInPast    = errors.New("conversation: snooze time must be in the future")
)

// MessageEvent is the payload published when a message is created.
//
// It is the wire shape realtime clients and webhook consumers both receive, so
// the field names are the API's, not the repository's. Anything not in here is
// not published — a payload is a publication boundary (§12).
type MessageEvent struct {
	ConversationID string  `json:"conversation_id"`
	InboxID        string  `json:"inbox_id"`
	MessageID      string  `json:"id"`
	ClientID       *string `json:"client_id,omitempty"`
	Kind           string  `json:"kind"`
	AuthorType     string  `json:"author_type"`
	AuthorID       *string `json:"author_id,omitempty"`
	AuthorName     string  `json:"author_name"`
	Body           string  `json:"body"`
	Sequence       int64   `json:"sequence"`
	CreatedAt      string  `json:"created_at"`
}

// ConversationEvent is the payload published when a conversation is created.
type ConversationEvent struct {
	ConversationID string  `json:"id"`
	InboxID        string  `json:"inbox_id"`
	Channel        string  `json:"channel"`
	Subject        *string `json:"subject,omitempty"`
	CustomerID     *string `json:"customer_id,omitempty"`
	State          string  `json:"state"`
}

type Service struct {
	repo   *repository
	pool   *database.Pool
	events *events.Log
	audit  *audit.Log
}

// New returns a Service. eventLog may be nil in tests that do not exercise
// publication; every publish site nil-checks through appendEvent.
func New(pool *database.Pool, eventLog *events.Log, auditLog *audit.Log) *Service {
	return &Service{repo: &repository{pool: pool}, pool: pool, events: eventLog, audit: auditLog}
}

// appendEvent records a state change on the workspace event log inside the
// caller's transaction.
//
// This replaces what used to be a direct call into the realtime gateway. The
// difference matters: a broadcast reaches whoever happens to be connected to
// this process right now, while an event is durable, ordered, and readable by
// every consumer that needs it — realtime resume, webhook delivery, automation
// triggers, notifications, and analytics. Publishing to five subsystems
// separately is how they drift; publishing once is how they cannot.
func (s *Service) appendEvent(ctx context.Context, tx pgx.Tx, event events.Event) error {
	if s.events == nil {
		return nil
	}
	_, err := s.events.Append(ctx, tx, event)
	return err
}

func (s *Service) recordAudit(ctx context.Context, tx pgx.Tx, entry audit.Entry) error {
	if s.audit == nil {
		return nil
	}
	if entry.ActorName == "" && entry.ActorType == audit.ActorUser && entry.ActorID != "" {
		if name, err := s.repo.memberDisplayName(ctx, tx, entry.ActorID); err == nil {
			entry.ActorName = name
		}
	}
	return audit.RecordTx(ctx, tx, entry)
}

// Start creates a new conversation with its opening message in one
// transaction, so a conversation can never exist without at least one
// message and vice versa.
func (s *Service) Start(
	ctx context.Context,
	workspaceID, inboxID, channel string,
	subject *string,
	customerID *string,
	visitorID *string,
	authorName, body string,
) (*Conversation, *Message, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil, ErrEmptyBody
	}

	conversationID := ids.New(ids.PrefixConversation)
	var message *Message

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.insert(ctx, tx, conversationID, workspaceID, inboxID, channel, subject, customerID, visitorID); err != nil {
			return err
		}
		if err := s.routeNewConversation(ctx, tx, workspaceID, conversationID, inboxID, customerID); err != nil {
			return err
		}

		if err := s.repo.lockConversation(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}

		// A visitor with no proven identity yet still speaks as "customer" —
		// there is no separate author_type for them (messages_author_type
		// CHECK constraint), and semantically they are exactly that: the
		// person on the other side of the conversation from an agent.
		authorType := "agent"
		if customerID != nil || visitorID != nil {
			authorType = "customer"
		}

		var err error
		message, err = s.repo.insertMessage(
			ctx, tx, ids.New(ids.PrefixMessage), nil,
			conversationID, workspaceID, "reply", authorType, customerID, authorName, body,
		)
		if err != nil {
			return err
		}

		if err := s.repo.touchConversation(ctx, tx, conversationID, preview(body), authorType == "customer", message.CreatedAt); err != nil {
			return err
		}

		if err := s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.ConversationCreated,
			EntityType:  entityConversation,
			EntityID:    conversationID,
			ActorType:   actorTypeFor(authorType),
			ActorID:     derefOr(customerID, ""),
			Data: ConversationEvent{
				ConversationID: conversationID,
				InboxID:        inboxID,
				Channel:        channel,
				Subject:        subject,
				CustomerID:     customerID,
				State:          "new",
			},
		}); err != nil {
			return err
		}

		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.MessageCreated,
			EntityType:  entityConversation,
			EntityID:    conversationID,
			ActorType:   actorTypeFor(authorType),
			ActorID:     derefOr(customerID, ""),
			Data:        messagePayload(inboxID, *message),
		})
	})
	if err != nil {
		return nil, nil, err
	}

	conv, err := s.repo.byID(ctx, workspaceID, conversationID)
	return conv, message, err
}

// PostMessage appends a reply or internal note to an existing conversation.
//
// clientID makes the call idempotent (§9): a retried send with the same
// clientID returns the message already written instead of creating a
// duplicate. The check-and-insert happens inside the same transaction that
// holds the conversation lock, so two racing retries cannot both pass the
// "does it exist" check and both insert.
func (s *Service) PostMessage(
	ctx context.Context,
	workspaceID, conversationID string,
	clientID *string,
	kind, authorType string,
	authorID *string,
	authorName, body string,
) (*Message, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, ErrEmptyBody
	}
	if kind == "" {
		kind = "reply"
	}

	var message *Message

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		conv, err := s.repo.lockAndLoad(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return err
		}

		if clientID != nil {
			existing, err := s.repo.messageByClientID(ctx, tx, conversationID, *clientID)
			if err != nil {
				return err
			}
			if existing != nil {
				// Idempotent replay (§9). Publishing again would deliver the
				// same message twice to every consumer, which is precisely
				// what the client id exists to prevent.
				message = existing
				return nil
			}
		}

		message, err = s.repo.insertMessage(
			ctx, tx, ids.New(ids.PrefixMessage), clientID,
			conversationID, workspaceID, kind, authorType, authorID, authorName, body,
		)
		if err != nil {
			return err
		}

		// Internal notes never touch the customer-facing preview or state —
		// they are invisible to the customer by definition (§6.2 composer).
		if kind == "reply" {
			if err := s.repo.touchConversation(ctx, tx, conversationID, preview(body), authorType == "customer", message.CreatedAt); err != nil {
				return err
			}
		}

		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.MessageCreated,
			EntityType:  entityConversation,
			EntityID:    conversationID,
			ActorType:   actorTypeFor(authorType),
			ActorID:     derefOr(authorID, ""),
			Data:        messagePayload(conv.InboxID, *message),
		})
	})
	if err != nil {
		return nil, err
	}

	return message, nil
}

// entityConversation is the entity type every conversation-scoped event
// carries. Realtime clients subscribe by "<entity_type>:<entity_id>", so this
// string is part of the wire contract, not an internal label.
const entityConversation = "conversation"

func messagePayload(inboxID string, message Message) MessageEvent {
	return MessageEvent{
		ConversationID: message.ConversationID,
		InboxID:        inboxID,
		MessageID:      message.ID,
		ClientID:       message.ClientID,
		Kind:           message.Kind,
		AuthorType:     message.AuthorType,
		AuthorID:       message.AuthorID,
		AuthorName:     message.AuthorName,
		Body:           message.Body,
		Sequence:       message.Sequence,
		CreatedAt:      message.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// actorTypeFor maps a message author to the event log's actor vocabulary.
// They are separate vocabularies on purpose: an event's actor may be an API
// key or an automation, neither of which can author a message.
func actorTypeFor(authorType string) events.ActorType {
	switch authorType {
	case "customer":
		return events.ActorCustomer
	case "agent":
		return events.ActorUser
	case "automation":
		return events.ActorAutomation
	default:
		return events.ActorSystem
	}
}

func derefOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

// Get returns one conversation, scoped to workspaceID.
func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Conversation, error) {
	return s.repo.byID(ctx, workspaceID, id)
}

// ConversationIDsForVisitor lists every conversation a visitor owns — the
// realtime grant their WebSocket connection needs (§9), since a visitor may
// have started more than one thread over time (e.g. a widget test call
// versus a return visit) and must be able to resume all of them.
func (s *Service) ConversationIDsForVisitor(ctx context.Context, workspaceID, visitorID string) ([]string, error) {
	return s.repo.idsForVisitor(ctx, workspaceID, visitorID)
}

// Messages returns the timeline for one conversation, optionally resuming
// after a given sequence — the same cursor the realtime gateway uses to
// replay missed events on reconnect (§9).
func (s *Service) Messages(ctx context.Context, workspaceID, conversationID string, afterSequence int64) ([]Message, error) {
	return s.repo.listMessages(ctx, workspaceID, conversationID, afterSequence)
}

// ListMessagesPage returns a bounded message window. A zero cursor starts at
// the newest messages; beforeSequence walks toward older history, while
// afterSequence is reserved for forward realtime replay.
func (s *Service) ListMessagesPage(ctx context.Context, workspaceID, conversationID string, beforeSequence, afterSequence int64, limit int) ([]Message, bool, error) {
	if beforeSequence > 0 && afterSequence > 0 {
		return nil, false, errors.New("conversation: before and after cursors are mutually exclusive")
	}
	return s.repo.listMessagesPage(ctx, workspaceID, conversationID, beforeSequence, afterSequence, limit)
}

func preview(body string) string {
	const max = 160
	body = strings.Join(strings.Fields(body), " ")
	if len(body) <= max {
		return body
	}
	return body[:max] + "…"
}
