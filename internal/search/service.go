package search

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/searchmodel"
	"regexp"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
)

var ErrBadCursor = errors.New("search: malformed cursor")

// Result is one hit in the global search — the CommandPalette's row shape,
// covering every entity kind search currently knows about. Tickets,
// articles, and feedback join this union as their modules land; nothing
// about this shape is conversation- or customer-specific.
type Result = searchmodel.Match

// Page is the global search response before the HTTP representation is
// applied. Messages are ordered before customers to preserve the command
// palette's existing grouping, and each group has its own stable cursor.
type Page struct {
	Results    []Result
	NextCursor string
	HasMore    bool
}

type cursor struct {
	Kind    string `json:"kind"`
	Query   string `json:"query,omitempty"`
	ExtraID string `json:"extra_id,omitempty"`

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
	providers    []Provider
}

func New(conversationService *conversation.Service, customerService *customer.Service, providers ...Provider) *Service {
	return &Service{conversation: conversationService, customer: customerService, providers: providers}
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
type Provider struct {
	Kind       string
	Capability authorization.Capability
	Fetch      func(context.Context, string, string, string, int, searchmodel.Filters) ([]Result, error)
}

func (s *Service) SearchPage(ctx context.Context, workspaceID, query, encodedCursor string, limit int) (Page, error) {
	return s.SearchFilteredPage(ctx, workspaceID, query, encodedCursor, limit, searchmodel.Filters{})
}

func (s *Service) SearchFilteredPage(ctx context.Context, workspaceID, query, encodedCursor string, limit int, filters searchmodel.Filters) (Page, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Page{Results: []Result{}}, nil
	}
	if len(query) > 256 {
		return Page{}, ErrBadCursor
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	position, err := decodeCursor(encodedCursor)
	if err != nil {
		return Page{}, err
	}
	binding, _ := json.Marshal(struct {
		Q string
		F searchmodel.Filters
	}{query, filters})
	if position.Query != "" && position.Query != string(binding) {
		return Page{}, ErrBadCursor
	}
	actor := authorization.FromContext(ctx)
	can := func(capability authorization.Capability) bool { return actor == nil || actor.Can(capability) }
	selected := func(kind string) bool {
		if len(filters.Kinds) == 0 {
			return true
		}
		for _, value := range filters.Kinds {
			if value == kind {
				return true
			}
		}
		return false
	}
	providers := []Provider{}
	if can(authorization.ConversationRead) && selected("message") {
		providers = append(providers, Provider{Kind: "messages", Fetch: func(ctx context.Context, ws, q, after string, n int, f searchmodel.Filters) ([]Result, error) {
			items, err := s.conversation.SearchMessagesPage(ctx, ws, q, position.MessageRank, position.MessageCreatedAt, position.MessageID, position.Kind == "messages" && after != "", n, f)
			if err != nil {
				return nil, err
			}
			results := messageResults(items)
			// Keep the precise ranked cursor alongside these message results.
			if n > 1 && len(items) > 0 {
				last := items[min(n-2, len(items)-1)]
				position.MessageRank = last.Rank
				position.MessageCreatedAt = last.CreatedAt
				position.MessageID = last.MessageID
			}
			return results, nil
		}})
	}
	if can(authorization.CustomerRead) && selected("customer") && filters.Channel == "" && filters.TagID == "" {
		providers = append(providers, Provider{Kind: "customers", Fetch: func(ctx context.Context, ws, q, after string, n int, f searchmodel.Filters) ([]Result, error) {
			items, err := s.customer.SearchPage(ctx, ws, q, position.CustomerLastSeenPresent, position.Kind == "customers" && position.CustomerID != "", position.CustomerLastSeen, position.CustomerFirstSeen, position.CustomerID, n, f)
			if err != nil {
				return nil, err
			}
			results := customerResults(items)
			if !can(authorization.CustomerReadSensitive) {
				for i := range results {
					results[i].Snippet = ""
				}
			}
			if n > 1 && len(items) > 0 {
				next := customerCursor(items[min(n-2, len(items)-1)])
				position.CustomerLastSeenPresent = next.CustomerLastSeenPresent
				position.CustomerLastSeen = next.CustomerLastSeen
				position.CustomerFirstSeen = next.CustomerFirstSeen
				position.CustomerID = next.CustomerID
			}
			return results, nil
		}})
	}
	for _, provider := range s.providers {
		if can(provider.Capability) && selected(provider.Kind) {
			if (filters.Channel != "" || filters.TagID != "") && provider.Kind != "ticket" {
				continue
			}
			providers = append(providers, provider)
		}
	}
	if matched, _ := regexp.MatchString(`^(#[0-9]+|[A-Za-z][A-Za-z0-9]*-[0-9]+)$`, query); matched {
		for i, provider := range providers {
			if provider.Kind == "ticket" {
				providers = append([]Provider{provider}, append(providers[:i], providers[i+1:]...)...)
				break
			}
		}
	}

	start := 0
	if position.Kind != "" {
		found := false
		for i, p := range providers {
			if p.Kind == position.Kind {
				start = i
				found = true
				break
			}
		}
		if !found {
			return Page{}, ErrBadCursor
		}
	}
	results := []Result{}
	for i := start; i < len(providers); i++ {
		provider := providers[i]
		remaining := limit - len(results)
		after := ""
		if position.Kind == provider.Kind {
			after = position.ExtraID
			if provider.Kind == "messages" {
				after = position.MessageID
			}
		}
		items, err := provider.Fetch(ctx, workspaceID, query, after, remaining+1, filters)
		if err != nil {
			return Page{}, err
		}
		if len(items) > remaining {
			items = items[:remaining]
			results = append(results, items...)
			if remaining == 0 {
				position = cursor{}
			}
			position.Kind = provider.Kind
			position.Query = string(binding)
			if len(items) > 0 {
				position.ExtraID = items[len(items)-1].EntityID
			}
			return Page{Results: results, NextCursor: encodeCursor(position), HasMore: true}, nil
		}
		results = append(results, items...)
	}
	return Page{Results: results}, nil
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
