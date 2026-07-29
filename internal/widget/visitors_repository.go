package widget

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Visitor struct {
	ID          string
	WorkspaceID string
	CustomerID  *string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

func scanVisitor(row interface{ Scan(dest ...any) error }) (*Visitor, error) {
	var v Visitor
	err := row.Scan(&v.ID, &v.WorkspaceID, &v.CustomerID, &v.FirstSeenAt, &v.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrVisitorInvalid
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *repository) insertVisitor(ctx context.Context, id, workspaceID string, tokenHash []byte) (*Visitor, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO visitors (id, workspace_id, token_hash)
		VALUES ($1, $2, $3)
		RETURNING id, workspace_id, customer_id, first_seen_at, last_seen_at
	`, id, workspaceID, tokenHash)
	return scanVisitor(row)
}

// visitorByTokenHash looks a visitor up by their hashed token, additionally
// touching last_seen_at — every authenticated widget request is a "this
// visitor is still here" signal, and there is no separate heartbeat call
// worth adding just to record that.
func (r *repository) visitorByTokenHash(ctx context.Context, workspaceID string, tokenHash []byte) (*Visitor, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE visitors SET last_seen_at = now()
		WHERE workspace_id = $1 AND token_hash = $2
		RETURNING id, workspace_id, customer_id, first_seen_at, last_seen_at
	`, workspaceID, tokenHash)
	return scanVisitor(row)
}

func (r *repository) visitorByID(ctx context.Context, workspaceID, id string) (*Visitor, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, customer_id, first_seen_at, last_seen_at
		FROM visitors WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	return scanVisitor(row)
}

// linkVisitor records the visitor→customer link inside the caller's
// transaction and updates the visitor's own customer_id so future lookups
// resolve directly, without walking visitor_customer_links every time.
func (r *repository) linkVisitor(ctx context.Context, tx pgx.Tx, linkID, visitorID, customerID, method string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO visitor_customer_links (id, visitor_id, customer_id, method)
		VALUES ($1, $2, $3, $4)
	`, linkID, visitorID, customerID, method); err != nil {
		return fmt.Errorf("widget: link visitor: %w", err)
	}
	_, err := tx.Exec(ctx, `UPDATE visitors SET customer_id = $2 WHERE id = $1`, visitorID, customerID)
	return err
}
