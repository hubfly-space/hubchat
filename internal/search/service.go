package search

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
)

var ErrBadCursor = errors.New("search: malformed cursor")

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

// Page is the global search response before the HTTP representation is
// applied. Messages are ordered before customers to preserve the command
// palette's existing grouping, and each group has its own stable cursor.
type Page struct {
	Results    []Result
	NextCursor string
	HasMore    bool
}

type cursor struct {
	Kind string `json:"kind"`

	MessageRank      float32   `json:"message_rank,omitempty"`
	MessageCreatedAt time.Time `json:"message_created_at,omitempty"`
	MessageID        string    `json:"message_id,omitempty"`

	CustomerLastSeenPresent bool      `json:"customer_last_seen_present,omitempty"`
	CustomerLastSeen        time.Time `json:"customer_last_seen,omitempty"`
	CustomerFirstSeen       time.Time `json:"customer_first_seen,omitempty"`
	CustomerID              string    `json:"customer_id,omitempty"`
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
	page, err := s.SearchPage(ctx, workspaceID, query, "", limitPerKind)
	if err != nil {
		return nil, err
	}
	return page.Results, nil
}

// SearchPage returns a total-limit page across message and customer hits.
// The cursor is opaque to callers and carries the last source-specific sort
// key, so equal full-text ranks and NULL customer timestamps cannot skip rows.
func (s *Service) SearchPage(ctx context.Context, workspaceID, query, encodedCursor string, limit int) (Page, error) {
	if query == "" {
		return Page{Results: []Result{}}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	position, err := decodeCursor(encodedCursor)
	if err != nil {
		return Page{}, err
	}
	if position.Kind != "" && position.Kind != "messages" && position.Kind != "customers" {
		return Page{}, ErrBadCursor
	}

	results := make([]Result, 0, limit)
	if position.Kind != "customers" {
		messageItems, err := s.conversation.SearchMessagesPage(ctx, workspaceID, query,
			position.MessageRank, position.MessageCreatedAt, position.MessageID,
			position.Kind == "messages", limit+1)
		if err != nil {
			return Page{}, err
		}
		if len(messageItems) > limit {
			messageItems = messageItems[:limit]
			last := messageItems[len(messageItems)-1]
			return Page{Results: messageResults(messageItems), NextCursor: encodeCursor(cursor{
				Kind: "messages", MessageRank: last.Rank, MessageCreatedAt: last.CreatedAt, MessageID: last.MessageID,
			}), HasMore: true}, nil
		}

		results = append(results, messageResults(messageItems)...)
		remaining := limit - len(results)
		if remaining == 0 {
			// Probe one customer so an exact message-sized page does not claim
			// to be terminal when the customer half still has matches.
			customers, err := s.customer.SearchPage(ctx, workspaceID, query, false, false, time.Time{}, time.Time{}, "", 1)
			if err != nil {
				return Page{}, err
			}
			if len(customers) > 0 {
				return Page{Results: results, NextCursor: encodeCursor(cursor{Kind: "customers"}), HasMore: true}, nil
			}
			return Page{Results: results}, nil
		}

		customers, err := s.customer.SearchPage(ctx, workspaceID, query, false, false, time.Time{}, time.Time{}, "", remaining+1)
		if err != nil {
			return Page{}, err
		}
		if len(customers) > remaining {
			customers = customers[:remaining]
			last := customers[len(customers)-1]
			results = append(results, customerResults(customers)...)
			return Page{Results: results, NextCursor: encodeCursor(customerCursor(last)), HasMore: true}, nil
		}
		results = append(results, customerResults(customers)...)
		return Page{Results: results}, nil
	}

	customerItems, err := s.customer.SearchPage(ctx, workspaceID, query,
		position.CustomerLastSeenPresent, position.CustomerID != "", position.CustomerLastSeen,
		position.CustomerFirstSeen, position.CustomerID, limit+1)
	if err != nil {
		return Page{}, err
	}
	if len(customerItems) > limit {
		customerItems = customerItems[:limit]
		last := customerItems[len(customerItems)-1]
		return Page{Results: customerResults(customerItems), NextCursor: encodeCursor(customerCursor(last)), HasMore: true}, nil
	}
	return Page{Results: customerResults(customerItems)}, nil
}

func messageResults(items []conversation.MessageSearchResult) []Result {
	out := make([]Result, 0, len(items))
	for _, m := range items {
		title := "Conversation"
		if m.ConversationSubj != nil && *m.ConversationSubj != "" {
			title = *m.ConversationSubj
		}
		out = append(out, Result{Kind: "message", Title: title, Snippet: m.Snippet, EntityID: m.MessageID, ConversationID: m.ConversationID})
	}
	return out
}

func customerResults(items []customer.SearchResult) []Result {
	out := make([]Result, 0, len(items))
	for _, c := range items {
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
	return out
}

func customerCursor(item customer.SearchResult) cursor {
	return cursor{Kind: "customers", CustomerLastSeenPresent: item.LastSeenPresent, CustomerLastSeen: item.LastSeenSort, CustomerFirstSeen: item.FirstSeenSort, CustomerID: item.ID}
}

func encodeCursor(value cursor) string {
	payload, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(value string) (cursor, error) {
	if value == "" {
		return cursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor{}, ErrBadCursor
	}
	var result cursor
	if err := json.Unmarshal(payload, &result); err != nil {
		return cursor{}, ErrBadCursor
	}
	return result, nil
}
