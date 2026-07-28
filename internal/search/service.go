package search

import (
	"context"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
)

// Result is one hit in the global search — the CommandPalette's row shape,
// covering every entity kind search currently knows about. Tickets,
// articles, and feedback join this union as their modules land; nothing
// about this shape is conversation- or customer-specific.
type Result struct {
	Kind    string // "message" | "customer"
	Title   string
	Snippet string
	// EntityID is the conversation or customer id a client navigates to.
	EntityID string
	// ConversationID is set only for message hits, since a message result
	// links to its conversation, not to itself.
	ConversationID string
}

// Service composes the owning modules' own search methods rather than
// querying their tables directly (§13 backend.md boundary) — this package
// holds no database handle of its own.
type Service struct {
	conversation *conversation.Service
	customer     *customer.Service
}

func New(conversationService *conversation.Service, customerService *customer.Service) *Service {
	return &Service{conversation: conversationService, customer: customerService}
}

// Search runs query against every entity kind currently searchable,
// returning a flat, ranked-within-kind list the CommandPalette groups by
// Kind for display (§6.17).
func (s *Service) Search(ctx context.Context, workspaceID, query string, limitPerKind int) ([]Result, error) {
	if query == "" {
		return []Result{}, nil
	}

	var out []Result

	messages, err := s.conversation.SearchMessages(ctx, workspaceID, query, limitPerKind)
	if err != nil {
		return nil, err
	}
	for _, m := range messages {
		title := "Conversation"
		if m.ConversationSubj != nil && *m.ConversationSubj != "" {
			title = *m.ConversationSubj
		}
		out = append(out, Result{
			Kind: "message", Title: title, Snippet: m.Snippet,
			EntityID: m.MessageID, ConversationID: m.ConversationID,
		})
	}

	customers, err := s.customer.Search(ctx, workspaceID, query, limitPerKind)
	if err != nil {
		return nil, err
	}
	for _, c := range customers {
		title := "Unnamed customer"
		if c.Name != nil && *c.Name != "" {
			title = *c.Name
		}
		snippet := ""
		if c.Email != nil {
			snippet = *c.Email
		}
		out = append(out, Result{Kind: "customer", Title: title, Snippet: snippet, EntityID: c.ID})
	}

	return out, nil
}
