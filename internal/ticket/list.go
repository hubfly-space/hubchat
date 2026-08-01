package ticket

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// UnassignedSentinel is what a caller passes for AssigneeID to mean "no
// assignee", the same convention conversation.ListFilter uses.
const UnassignedSentinel = "unassigned"

// ListFilter narrows the ticket queue. Every field left at its zero value is
// simply not filtered on.
type ListFilter struct {
	Query      string
	InboxID    string
	AssigneeID string
	TeamID     string
	CustomerID string
	CompanyID  string
	ParentID   string
	FollowerID string
	TagID      string
	Priority   string
	// Status defaults to "every status but closed" when empty — the same set
	// tickets_active_queue indexes.
	Status []string

	Before   time.Time
	BeforeID string
	Limit    int
}

// List returns one page of tickets matching filter, most-recently-updated
// first, tie-broken by id so pagination is exact under concurrent writes.
func (s *Service) List(ctx context.Context, workspaceID string, filter ListFilter) ([]Ticket, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	return s.repo.list(ctx, workspaceID, filter)
}

func (r *repository) list(ctx context.Context, workspaceID string, filter ListFilter) ([]Ticket, error) {
	var (
		where []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	where = append(where, "workspace_id = "+arg(workspaceID))

	if filter.InboxID != "" {
		where = append(where, "inbox_id = "+arg(filter.InboxID))
	}
	if strings.TrimSpace(filter.Query) != "" {
		query := strings.TrimSpace(filter.Query)
		where = append(where, "(title ILIKE '%' || "+arg(query)+" || '%' OR coalesce(description, '') ILIKE '%' || "+arg(query)+" || '%')")
	}
	switch filter.AssigneeID {
	case "":
	case UnassignedSentinel:
		where = append(where, "assignee_id IS NULL")
	default:
		where = append(where, "assignee_id = "+arg(filter.AssigneeID))
	}
	if filter.TeamID != "" {
		where = append(where, "team_id = "+arg(filter.TeamID))
	}
	if filter.CustomerID != "" {
		where = append(where, "customer_id = "+arg(filter.CustomerID))
	}
	if filter.CompanyID != "" {
		where = append(where, "company_id = "+arg(filter.CompanyID))
	}
	if filter.ParentID != "" {
		where = append(where, "parent_id = "+arg(filter.ParentID))
	}
	if filter.Priority != "" {
		where = append(where, "priority = "+arg(filter.Priority))
	}
	if len(filter.Status) > 0 {
		placeholders := make([]string, len(filter.Status))
		for i, status := range filter.Status {
			placeholders[i] = arg(status)
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ", ")+")")
	} else {
		where = append(where, "status <> 'closed'")
	}
	if filter.TagID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM ticket_tags tt WHERE tt.ticket_id = tickets.id AND tt.tag_id = "+arg(filter.TagID)+")")
	}
	if filter.FollowerID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM ticket_followers tf WHERE tf.ticket_id = tickets.id AND tf.member_id = "+arg(filter.FollowerID)+")")
	}

	before := filter.Before
	if before.IsZero() {
		before = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	where = append(where, "(updated_at, id) < ("+arg(before)+", "+arg(filter.BeforeID)+")")

	limitPlaceholder := arg(filter.Limit)

	query := `SELECT ` + ticketColumns + `
		FROM tickets
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY updated_at DESC, id DESC
		LIMIT ` + limitPlaceholder

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ticket: list: %w", err)
	}
	defer rows.Close()

	out := []Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}
