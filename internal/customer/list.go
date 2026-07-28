package customer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ListFilter narrows the customer directory (§6.9). Every field left at its
// zero value is simply not filtered on.
type ListFilter struct {
	Query        string
	TagID        string
	CompanyID    string
	Verification string

	Before   time.Time
	BeforeID string
	Limit    int
}

// List returns one page of customers, most-recently-seen first, tie-broken
// by id so pagination is exact under concurrent writes (§16) — the
// customer directory's server-driven counterpart to Search, which stays a
// small unpaginated picker lookup.
func (s *Service) List(ctx context.Context, workspaceID string, filter ListFilter) ([]Customer, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	return s.repo.list(ctx, workspaceID, filter)
}

func (r *repository) list(ctx context.Context, workspaceID string, filter ListFilter) ([]Customer, error) {
	var (
		where []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	where = append(where, "workspace_id = "+arg(workspaceID))

	if filter.Query != "" {
		where = append(where, "(coalesce(name, '') || ' ' || coalesce(email::text, '')) ILIKE '%' || "+arg(filter.Query)+" || '%'")
	}
	if filter.Verification != "" {
		where = append(where, "verification = "+arg(filter.Verification))
	}
	if filter.TagID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM customer_tags ct WHERE ct.customer_id = customers.id AND ct.tag_id = "+arg(filter.TagID)+")")
	}
	if filter.CompanyID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM company_customers cc WHERE cc.customer_id = customers.id AND cc.company_id = "+arg(filter.CompanyID)+")")
	}

	before := filter.Before
	if before.IsZero() {
		before = time.Now().Add(time.Hour)
	}
	// last_seen_at is nullable (a customer who has never been seen since
	// creation); coalescing to first_seen_at keeps the sort total and the
	// cursor comparison well-defined either way.
	where = append(where, "(coalesce(last_seen_at, first_seen_at), id) < ("+arg(before)+", "+arg(filter.BeforeID)+")")

	limitPlaceholder := arg(filter.Limit)

	query := `SELECT ` + customerColumns + `
		FROM customers
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY coalesce(last_seen_at, first_seen_at) DESC, id DESC
		LIMIT ` + limitPlaceholder

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("customer: list: %w", err)
	}
	defer rows.Close()

	out := []Customer{}
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
