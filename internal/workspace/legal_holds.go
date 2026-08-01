package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrLegalHoldNotFound = errors.New("workspace: legal hold not found")
	ErrInvalidLegalHold  = errors.New("workspace: invalid legal hold")
)

// LegalHold is an append-only retention override. A released hold remains in
// the history so operators can prove when protection was active.
type LegalHold struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	Category    string     `json:"category"`
	Reason      string     `json:"reason"`
	CreatedBy   *string    `json:"created_by,omitempty"`
	ReleasedBy  *string    `json:"released_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ReleasedAt  *time.Time `json:"released_at,omitempty"`
}

type LegalHoldInput struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

func validateLegalHold(input LegalHoldInput) error {
	validCategories := map[string]bool{
		"all": true, "events": true, "sessions": true,
		"webhooks": true, "surveys": true, "audit": true,
	}
	if !validCategories[input.Category] {
		return fmt.Errorf("%w: category must be all, events, sessions, webhooks, surveys, or audit", ErrInvalidLegalHold)
	}
	if reason := strings.TrimSpace(input.Reason); reason == "" || len(reason) > 500 {
		return fmt.Errorf("%w: reason must be between 1 and 500 characters", ErrInvalidLegalHold)
	}
	return nil
}

// CreateLegalHold protects one retention category until it is explicitly
// released. The hold and its audit record commit together.
func (s *Service) CreateLegalHold(ctx context.Context, workspaceID, actorMemberID string, input LegalHoldInput) (*LegalHold, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if err := validateLegalHold(input); err != nil {
		return nil, err
	}

	id := ids.New(ids.PrefixLegalHold)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO workspace_legal_holds (id, workspace_id, category, reason, created_by)
			VALUES ($1, $2, $3, $4, $5)
		`, id, workspaceID, input.Category, input.Reason, actorMemberID); err != nil {
			return fmt.Errorf("workspace: create legal hold: %w", err)
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.LegalHoldCreated, EntityType: "legal_hold", EntityID: id,
			Metadata: map[string]any{"category": input.Category},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.legalHoldByID(ctx, workspaceID, id)
}

// ReleaseLegalHold removes the retention override while preserving its audit
// history. The workspace predicate is intentionally part of the UPDATE.
func (s *Service) ReleaseLegalHold(ctx context.Context, workspaceID, actorMemberID, holdID string) (*LegalHold, error) {
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE workspace_legal_holds
			SET released_by = $1, released_at = now()
			WHERE workspace_id = $2 AND id = $3 AND released_at IS NULL
		`, actorMemberID, workspaceID, holdID)
		if err != nil {
			return fmt.Errorf("workspace: release legal hold: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrLegalHoldNotFound
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.LegalHoldReleased, EntityType: "legal_hold", EntityID: holdID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.legalHoldByID(ctx, workspaceID, holdID)
}

// ListLegalHoldsPage returns a stable newest-first history. By default only
// active holds are shown; includeReleased makes the same endpoint useful for
// an auditable operations history screen.
func (s *Service) ListLegalHoldsPage(ctx context.Context, workspaceID string, includeReleased bool, before time.Time, beforeID string, limit int) ([]LegalHold, error) {
	if limit <= 0 || limit > 201 {
		limit = 50
	}
	if before.IsZero() {
		before = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, category, reason, created_by, released_by, created_at, released_at
		FROM workspace_legal_holds
		WHERE workspace_id = $1
		  AND ($2 OR released_at IS NULL)
		  AND (created_at, id) < ($3, $4)
		ORDER BY created_at DESC, id DESC
		LIMIT $5
	`, workspaceID, includeReleased, before, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("workspace: list legal holds: %w", err)
	}
	defer rows.Close()

	result := make([]LegalHold, 0)
	for rows.Next() {
		var item LegalHold
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Category, &item.Reason, &item.CreatedBy, &item.ReleasedBy, &item.CreatedAt, &item.ReleasedAt); err != nil {
			return nil, fmt.Errorf("workspace: scan legal hold: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("workspace: legal hold rows: %w", err)
	}
	return result, nil
}

func (s *Service) legalHoldByID(ctx context.Context, workspaceID, holdID string) (*LegalHold, error) {
	var item LegalHold
	err := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, category, reason, created_by, released_by, created_at, released_at
		FROM workspace_legal_holds WHERE workspace_id = $1 AND id = $2
	`, workspaceID, holdID).Scan(&item.ID, &item.WorkspaceID, &item.Category, &item.Reason, &item.CreatedBy, &item.ReleasedBy, &item.CreatedAt, &item.ReleasedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLegalHoldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: get legal hold: %w", err)
	}
	return &item, nil
}
