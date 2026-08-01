package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// unassignedSentinel is what a caller passes for AssigneeID to mean "no
// assignee", distinguishing it from "" meaning "any assignee, filter not
// applied" — a nullable column needs a value outside its own domain to ask
// for the null case explicitly.
const UnassignedSentinel = "unassigned"

// ListFilter narrows the conversation list. Every field left at its zero
// value is simply not filtered on, so the same query serves the unfiltered
// "everything in this inbox" view and every narrowed one.
type ListFilter struct {
	InboxID    string
	AssigneeID string
	TeamID     string
	CustomerID string
	// States defaults to "every open state" (not closed, not spam) when empty
	// — the same set conversations_active_queue indexes.
	States     []string
	Priority   string
	TagID      string
	FollowerID string

	Before   time.Time
	BeforeID string
	Limit    int
}

// List returns one page of conversations matching filter, newest activity
// first, tie-broken by id so pagination is exact under concurrent writes
// (§16).
func (s *Service) List(ctx context.Context, workspaceID string, filter ListFilter) ([]Conversation, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	return s.repo.list(ctx, workspaceID, filter)
}

func (r *repository) list(ctx context.Context, workspaceID string, filter ListFilter) ([]Conversation, error) {
	var (
		where []string
		args  []any
	)

	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	where = append(where, "c.workspace_id = "+arg(workspaceID))

	if filter.InboxID != "" {
		where = append(where, "c.inbox_id = "+arg(filter.InboxID))
	}

	switch filter.AssigneeID {
	case "":
		// no filter
	case UnassignedSentinel:
		where = append(where, "c.assignee_id IS NULL")
	default:
		where = append(where, "c.assignee_id = "+arg(filter.AssigneeID))
	}

	if filter.TeamID != "" {
		where = append(where, "c.team_id = "+arg(filter.TeamID))
	}

	if filter.CustomerID != "" {
		where = append(where, "c.customer_id = "+arg(filter.CustomerID))
	}

	if filter.Priority != "" {
		where = append(where, "c.priority = "+arg(filter.Priority))
	}

	if len(filter.States) > 0 {
		placeholders := make([]string, len(filter.States))
		for i, state := range filter.States {
			placeholders[i] = arg(state)
		}
		where = append(where, "c.state IN ("+strings.Join(placeholders, ", ")+")")
	} else {
		where = append(where, "c.state NOT IN ('closed', 'spam')")
	}

	if filter.TagID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM conversation_tags ct WHERE ct.conversation_id = c.id AND ct.tag_id = "+arg(filter.TagID)+")")
	}

	if filter.FollowerID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM conversation_followers cf WHERE cf.conversation_id = c.id AND cf.member_id = "+arg(filter.FollowerID)+")")
	}

	before := filter.Before
	if before.IsZero() {
		before = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	where = append(where, "(c.last_message_at, c.id) < ("+arg(before)+", "+arg(filter.BeforeID)+")")

	limitPlaceholder := arg(filter.Limit)

	// Column list is qualified with c. here (unlike conversationColumns,
	// which assumes an unaliased FROM conversations) since this query always
	// has the c alias for the WHERE/ORDER BY clauses above.
	query := `
		SELECT c.id, c.workspace_id, c.inbox_id, c.channel, c.subject, c.state, c.priority,
		       c.customer_id, c.visitor_id, c.assignee_id, c.team_id, c.ticket_id, c.message_count,
		       c.last_message_preview, c.last_message_at, c.last_customer_at, c.snoozed_until, c.created_at
		FROM conversations c
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY c.last_message_at DESC, c.id DESC
		LIMIT ` + limitPlaceholder

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("conversation: list: %w", err)
	}
	defer rows.Close()

	out := []Conversation{}
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
