package widget

import (
	"context"
	"errors"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
)

var ErrNoInbox = errors.New("widget: this widget has no destination inbox configured")

// StartConversation opens a new conversation on the visitor's behalf,
// routed to the widget's configured inbox.
func (s *Service) StartConversation(ctx context.Context, workspaceID string, w *Widget, visitor *Visitor, body string) (*conversation.Conversation, *conversation.Message, error) {
	if !w.Enabled {
		return nil, nil, ErrDisabled
	}
	if w.InboxID == nil {
		return nil, nil, ErrNoInbox
	}
	return s.conversation.Start(
		ctx, workspaceID, *w.InboxID, "widget", nil,
		visitor.CustomerID, &visitor.ID, s.visitorAuthorName(ctx, workspaceID, visitor), body,
	)
}

// PostMessage appends a reply from the visitor to an existing conversation,
// after confirming it is actually theirs — a visitor token authenticates the
// visitor, not any conversation id they happen to send.
func (s *Service) PostMessage(ctx context.Context, workspaceID, conversationID string, visitor *Visitor, body string) (*conversation.Message, error) {
	if err := s.checkOwnership(ctx, workspaceID, conversationID, visitor); err != nil {
		return nil, err
	}
	return s.conversation.PostMessage(
		ctx, workspaceID, conversationID, nil, "reply", "customer",
		visitor.CustomerID, s.visitorAuthorName(ctx, workspaceID, visitor), body,
	)
}

// Conversation returns one conversation, after the same ownership check
// PostMessage applies.
func (s *Service) Conversation(ctx context.Context, workspaceID, conversationID string, visitor *Visitor) (*conversation.Conversation, error) {
	if err := s.checkOwnership(ctx, workspaceID, conversationID, visitor); err != nil {
		return nil, err
	}
	return s.conversation.Get(ctx, workspaceID, conversationID)
}

// Messages returns a conversation's timeline for the widget to render or
// resume — used both for "load history on reopen" (when
// behavior.persist_conversation is set) and as the HTTP fallback for
// messages that arrived while the visitor's socket was disconnected.
func (s *Service) Messages(ctx context.Context, workspaceID, conversationID string, visitor *Visitor, afterSequence int64) ([]conversation.Message, error) {
	if err := s.checkOwnership(ctx, workspaceID, conversationID, visitor); err != nil {
		return nil, err
	}
	all, err := s.conversation.Messages(ctx, workspaceID, conversationID, afterSequence)
	if err != nil {
		return nil, err
	}

	// Internal notes are never for the visitor's eyes (mirrors the same cut
	// realtime's hub.go makes for the live socket) — an agent's note written
	// assuming it is invisible to the customer must not reach this response
	// just because it shares a table with the replies that are.
	visible := make([]conversation.Message, 0, len(all))
	for _, m := range all {
		if m.Kind == "note" {
			continue
		}
		visible = append(visible, m)
	}
	return visible, nil
}

func (s *Service) checkOwnership(ctx context.Context, workspaceID, conversationID string, visitor *Visitor) error {
	conv, err := s.conversation.Get(ctx, workspaceID, conversationID)
	if err != nil {
		return err
	}
	if conv.VisitorID == nil || *conv.VisitorID != visitor.ID {
		return ErrConversationOwner
	}
	return nil
}

// visitorAuthorName is what appears as the message author in the agent
// inbox. A linked customer's own name is used once one exists; until then
// there is nothing to show but the generic label every anonymous visitor
// shares.
func (s *Service) visitorAuthorName(ctx context.Context, workspaceID string, visitor *Visitor) string {
	if visitor.CustomerID == nil {
		return "Visitor"
	}
	cust, err := s.customer.Get(ctx, workspaceID, *visitor.CustomerID)
	if err != nil || cust.Name == nil || *cust.Name == "" {
		return "Visitor"
	}
	return *cust.Name
}

// Track ingests one application event from the widget/SDK — the same path
// the authenticated REST endpoint uses, attributed to this visitor's linked
// customer when one exists and left anonymous otherwise (IngestEvent
// tolerates an empty customer id for exactly this case).
func (s *Service) Track(ctx context.Context, workspaceID string, visitor *Visitor, eventType string, pageURL *string, payload map[string]any) (*customer.CustomerEvent, error) {
	customerID := ""
	if visitor.CustomerID != nil {
		customerID = *visitor.CustomerID
	}
	return s.customer.IngestEvent(ctx, workspaceID, customerID, eventType, "js_sdk", pageURL, payload)
}
