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

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrEmptyBody = errors.New("conversation: message body must not be empty")
)

// Broadcaster is the seam between this module and realtime (§14 boundary
// rules: cross-module work is an explicit call, not a shared table). The
// realtime gateway implements it; conversation does not know anything about
// WebSocket connections.
type Broadcaster interface {
	BroadcastMessage(workspaceID, conversationID string, message Message)
}

// noopBroadcaster is used when realtime is disabled, so PostMessage never has
// to nil-check.
type noopBroadcaster struct{}

func (noopBroadcaster) BroadcastMessage(string, string, Message) {}

type Service struct {
	repo        *repository
	pool        *database.Pool
	broadcaster Broadcaster
}

func New(pool *database.Pool) *Service {
	return &Service{repo: &repository{pool: pool}, pool: pool, broadcaster: noopBroadcaster{}}
}

// SetBroadcaster wires the realtime gateway in. Called once at startup from
// cmd/hubchat, after both modules exist — see the app package's construction
// order.
func (s *Service) SetBroadcaster(b Broadcaster) {
	if b == nil {
		b = noopBroadcaster{}
	}
	s.broadcaster = b
}

// Start creates a new conversation with its opening message in one
// transaction, so a conversation can never exist without at least one
// message and vice versa.
func (s *Service) Start(
	ctx context.Context,
	workspaceID, inboxID, channel string,
	subject *string,
	customerID *string,
	authorName, body string,
) (*Conversation, *Message, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil, ErrEmptyBody
	}

	conversationID := ids.New(ids.PrefixConversation)
	var message *Message

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.insert(ctx, conversationID, workspaceID, inboxID, channel, subject, customerID); err != nil {
			return err
		}

		if err := s.repo.lockConversation(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}

		authorType := "agent"
		if customerID != nil {
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

		return s.repo.touchConversation(ctx, tx, conversationID, preview(body), authorType == "customer", message.CreatedAt)
	})
	if err != nil {
		return nil, nil, err
	}

	s.broadcaster.BroadcastMessage(workspaceID, conversationID, *message)

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
		if err := s.repo.lockConversation(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}

		if clientID != nil {
			existing, err := s.repo.messageByClientID(ctx, tx, conversationID, *clientID)
			if err != nil {
				return err
			}
			if existing != nil {
				message = existing
				return nil // idempotent replay — nothing new to broadcast or touch
			}
		}

		var err error
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
			return s.repo.touchConversation(ctx, tx, conversationID, preview(body), authorType == "customer", message.CreatedAt)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.broadcaster.BroadcastMessage(workspaceID, conversationID, *message)
	return message, nil
}

// Get returns one conversation, scoped to workspaceID.
func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Conversation, error) {
	return s.repo.byID(ctx, workspaceID, id)
}

// ListActive returns the open queue for one inbox (the conversations_active_queue index).
func (s *Service) ListActive(ctx context.Context, workspaceID, inboxID string, limit int) ([]Conversation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.listActive(ctx, workspaceID, inboxID, limit)
}

// Messages returns the timeline for one conversation, optionally resuming
// after a given sequence — the same cursor the realtime gateway uses to
// replay missed events on reconnect (§9).
func (s *Service) Messages(ctx context.Context, workspaceID, conversationID string, afterSequence int64) ([]Message, error) {
	return s.repo.listMessages(ctx, workspaceID, conversationID, afterSequence)
}

func preview(body string) string {
	const max = 160
	body = strings.Join(strings.Fields(body), " ")
	if len(body) <= max {
		return body
	}
	return body[:max] + "…"
}
