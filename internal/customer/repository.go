package customer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
)

var (
	ErrNotFound        = errors.New("customer: not found")
	ErrVersionConflict = errors.New("customer: this profile changed since you loaded it")
	ErrTagNotFound     = errors.New("customer: not a tag in this workspace")
)

type Customer struct {
	ID              string
	WorkspaceID     string
	Name            *string
	Email           *string
	Phone           *string
	AvatarURL       *string
	ExternalID      *string
	Verification    string
	Language        *string
	Timezone        *string
	Attributes      map[string]any
	OwnerID         *string
	FirstSeenAt     time.Time
	LastSeenAt      *time.Time
	LastContactedAt *time.Time
	Version         int
}

type repository struct {
	pool *database.Pool
}

const customerColumns = `
	id, workspace_id, name, email::text, phone, avatar_url, external_id, verification,
	language, timezone, attributes, owner_id, first_seen_at, last_seen_at, last_contacted_at, version
`

func scanCustomer(row interface{ Scan(dest ...any) error }) (*Customer, error) {
	var c Customer
	err := row.Scan(
		&c.ID, &c.WorkspaceID, &c.Name, &c.Email, &c.Phone, &c.AvatarURL, &c.ExternalID, &c.Verification,
		&c.Language, &c.Timezone, &c.Attributes, &c.OwnerID, &c.FirstSeenAt, &c.LastSeenAt, &c.LastContactedAt, &c.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.Attributes == nil {
		c.Attributes = map[string]any{}
	}
	return &c, nil
}

func (r *repository) byID(ctx context.Context, workspaceID, id string) (*Customer, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+customerColumns+`
		FROM customers WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	return scanCustomer(row)
}

// byIDs batch-loads customers for a page of conversation rows in one query —
// the inbox list's alternative to each row fetching its own customer, which
// for fifty rows would be fifty round trips for what is otherwise one list
// call.
func (r *repository) byIDs(ctx context.Context, workspaceID string, ids []string) ([]Customer, error) {
	if len(ids) == 0 {
		return []Customer{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+customerColumns+`
		FROM customers WHERE workspace_id = $1 AND id = ANY($2)
	`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("customer: get many: %w", err)
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

// update applies the caller's fields under an optimistic-concurrency check
// (§13 conventions): the row's version must match expectedVersion, or two
// agents editing the same profile at once would silently overwrite one
// another instead of the second save failing loudly.
func (r *repository) update(
	ctx context.Context, workspaceID, id string, expectedVersion int,
	name, email, phone, language, timezone *string,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE customers
		SET name = $3, email = $4, phone = $5, language = $6, timezone = $7, version = version + 1
		WHERE workspace_id = $1 AND id = $2 AND version = $8
	`, workspaceID, id, name, email, phone, language, timezone, expectedVersion)
	if err != nil {
		return fmt.Errorf("customer: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		exists, existsErr := r.exists(ctx, workspaceID, id)
		if existsErr != nil {
			return existsErr
		}
		if !exists {
			return ErrNotFound
		}
		return ErrVersionConflict
	}
	return nil
}

func (r *repository) exists(ctx context.Context, workspaceID, id string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM customers WHERE workspace_id = $1 AND id = $2)
	`, workspaceID, id).Scan(&exists)
	return exists, err
}

// search matches the customers_search trigram index verbatim (migration
// 0002): name and email, case-insensitive, substring-tolerant.
func (r *repository) search(ctx context.Context, workspaceID, query string, limit int) ([]Customer, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+customerColumns+`
		FROM customers
		WHERE workspace_id = $1
		  AND ($2 = '' OR (coalesce(name, '') || ' ' || coalesce(email::text, '')) ILIKE '%' || $2 || '%')
		ORDER BY last_seen_at DESC NULLS LAST, first_seen_at DESC
		LIMIT $3
	`, workspaceID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("customer: search: %w", err)
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

func (r *repository) tagInWorkspace(ctx context.Context, workspaceID, tagID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tags WHERE id = $1 AND workspace_id = $2)
	`, tagID, workspaceID).Scan(&exists)
	return exists, err
}

func (r *repository) addTag(ctx context.Context, customerID, tagID string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO customer_tags (customer_id, tag_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, customerID, tagID)
	return err
}

func (r *repository) removeTag(ctx context.Context, customerID, tagID string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM customer_tags WHERE customer_id = $1 AND tag_id = $2
	`, customerID, tagID)
	return err
}

func (r *repository) tagIDs(ctx context.Context, workspaceID, customerID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ct.tag_id FROM customer_tags ct
		JOIN customers c ON c.id = ct.customer_id
		WHERE c.workspace_id = $1 AND ct.customer_id = $2
	`, workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *repository) companyIDs(ctx context.Context, workspaceID, customerID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT cc.company_id FROM company_customers cc
		JOIN customers c ON c.id = cc.customer_id
		WHERE c.workspace_id = $1 AND cc.customer_id = $2
	`, workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// blockContact records a block (§6.9-adjacent moderation action). kind and
// value mirror blocked_contacts' CHECK constraint (migration 0006):
// 'email' | 'domain' | 'ip' | 'visitor' | 'customer'.
func (r *repository) blockContact(ctx context.Context, id, workspaceID, kind, value string, reason *string, blockedBy string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO blocked_contacts (id, workspace_id, kind, value, reason, blocked_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (workspace_id, kind, value) DO UPDATE SET reason = $5, blocked_by = $6
	`, id, workspaceID, kind, value, reason, blockedBy)
	return err
}

func (r *repository) memberDisplayName(ctx context.Context, memberID string) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `
		SELECT u.name FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.id = $1
	`, memberID).Scan(&name)
	return name, err
}
