package ticket

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
)

var (
	ErrNotFound        = errors.New("ticket: not found")
	ErrVersionConflict = errors.New("ticket: this ticket changed since you loaded it")
)

type Ticket struct {
	ID              string
	WorkspaceID     string
	Number          int
	Prefix          string
	Title           string
	Description     string
	Status          string
	Priority        string
	Type            *string
	CustomerID      *string
	CompanyID       *string
	InboxID         *string
	Channel         string
	AssigneeID      *string
	TeamID          *string
	ConversationID  *string
	ParentID        *string
	SLAPolicyID     *string
	DueAt           *time.Time
	FirstResolvedAt *time.Time
	ResolvedAt      *time.Time
	ClosedAt        *time.Time
	ReopenCount     int
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ticketColumns is the column list every full-row Ticket query shares, kept
// in one place so byID, list, and every mutation's reload can never drift
// from each other's scan order.
const ticketColumns = `
	id, workspace_id, number, prefix, title, description, status, priority, type,
	customer_id, company_id, inbox_id, channel, assignee_id, team_id, conversation_id,
	parent_id, sla_policy_id, due_at, first_resolved_at, resolved_at, closed_at,
	reopen_count, version, created_at, updated_at
`

func scanTicket(row interface{ Scan(dest ...any) error }) (*Ticket, error) {
	var t Ticket
	err := row.Scan(
		&t.ID, &t.WorkspaceID, &t.Number, &t.Prefix, &t.Title, &t.Description, &t.Status, &t.Priority, &t.Type,
		&t.CustomerID, &t.CompanyID, &t.InboxID, &t.Channel, &t.AssigneeID, &t.TeamID, &t.ConversationID,
		&t.ParentID, &t.SLAPolicyID, &t.DueAt, &t.FirstResolvedAt, &t.ResolvedAt, &t.ClosedAt,
		&t.ReopenCount, &t.Version, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

type repository struct {
	pool *database.Pool
}

// insert writes the ticket row inside the caller's transaction, so it always
// commits alongside the number allocation that produced number/prefix — a
// ticket can never exist with a number nobody allocated, or vice versa.
func (r *repository) insert(
	ctx context.Context, tx pgx.Tx,
	id, workspaceID string, number int, prefix, title, description, priority string,
	ttype, customerID, companyID, inboxID *string, channel string,
	conversationID, parentID *string, dueAt *time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tickets
			(id, workspace_id, number, prefix, title, description, status, priority, type,
			 customer_id, company_id, inbox_id, channel, conversation_id, parent_id, due_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'new', $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, id, workspaceID, number, prefix, title, description, priority, ttype,
		customerID, companyID, inboxID, channel, conversationID, parentID, dueAt)
	return err
}

func (r *repository) byID(ctx context.Context, workspaceID, id string) (*Ticket, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+ticketColumns+`
		FROM tickets WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	return scanTicket(row)
}

func (r *repository) exists(ctx context.Context, workspaceID, id string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tickets WHERE workspace_id = $1 AND id = $2)
	`, workspaceID, id).Scan(&exists)
	return exists, err
}

// updateDetails changes the free-text/classification fields under optimistic
// concurrency (§13 conventions): expectedVersion must match what is
// currently stored, or two agents editing the same ticket at once would
// silently overwrite one another instead of the second save failing loudly.
func (r *repository) updateDetails(
	ctx context.Context, workspaceID, id string, expectedVersion int,
	title, description string, ttype *string, dueAt *time.Time,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tickets
		SET title = $3, description = $4, type = $5, due_at = $6, version = version + 1, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND version = $7
	`, workspaceID, id, title, description, ttype, dueAt, expectedVersion)
	if err != nil {
		return fmt.Errorf("ticket: update details: %w", err)
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

// memberDisplayName resolves a member id to the user's current name, for
// denormalising into an audit entry at write time (mirrors the identical
// query in conversation/customer/inbox — see any of those for why this is
// duplicated rather than shared).
func (r *repository) memberDisplayName(ctx context.Context, memberID string) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `
		SELECT u.name FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.id = $1
	`, memberID).Scan(&name)
	return name, err
}
