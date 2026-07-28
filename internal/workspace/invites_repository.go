package workspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// errUniqueInvite signals the (workspace_id, email) unique index rejected an
// insert — translated to the exported ErrInviteExists at the service layer.
var errUniqueInvite = errors.New("workspace: duplicate invite")

func (r *repository) emailIsMember(ctx context.Context, workspaceID, email string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM workspace_members m
			JOIN users u ON u.id = m.user_id
			WHERE m.workspace_id = $1 AND u.email = $2
		)
	`, workspaceID, email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("workspace: check existing membership: %w", err)
	}
	return exists, nil
}

func (r *repository) insertInvite(
	ctx context.Context, tx pgx.Tx,
	id, workspaceID, email, role, invitedByMemberID string,
	tokenHash []byte, expiresAt time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO workspace_invites (id, workspace_id, email, role, token_hash, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, workspaceID, email, role, tokenHash, invitedByMemberID, expiresAt)
	if err != nil && isUniqueViolation(err) {
		return errUniqueInvite
	}
	if err != nil {
		return fmt.Errorf("workspace: insert invite: %w", err)
	}
	return nil
}

func (r *repository) inviteByID(ctx context.Context, workspaceID, id string) (*Invite, error) {
	var invite Invite
	err := r.pool.QueryRow(ctx, `
		SELECT id, email::text, role, invited_by, expires_at, accepted_at, created_at
		FROM workspace_invites
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id).Scan(
		&invite.ID, &invite.Email, &invite.Role, &invite.InvitedBy,
		&invite.ExpiresAt, &invite.AcceptedAt, &invite.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: load invite: %w", err)
	}
	return &invite, nil
}

func (r *repository) listInvites(ctx context.Context, workspaceID string) ([]Invite, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, email::text, role, invited_by, expires_at, accepted_at, created_at
		FROM workspace_invites
		WHERE workspace_id = $1
		ORDER BY created_at DESC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace: list invites: %w", err)
	}
	defer rows.Close()

	out := []Invite{}
	for rows.Next() {
		var invite Invite
		if err := rows.Scan(
			&invite.ID, &invite.Email, &invite.Role, &invite.InvitedBy,
			&invite.ExpiresAt, &invite.AcceptedAt, &invite.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, invite)
	}
	return out, rows.Err()
}

func (r *repository) deleteInvite(ctx context.Context, tx pgx.Tx, workspaceID, inviteID string) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM workspace_invites WHERE workspace_id = $1 AND id = $2
	`, workspaceID, inviteID)
	if err != nil {
		return fmt.Errorf("workspace: revoke invite: %w", err)
	}
	return nil
}

// inviteDetailsByTokenHash resolves a token for the unauthenticated
// accept-invite page — enough to render, nothing more.
func (r *repository) inviteDetailsByTokenHash(ctx context.Context, tokenHash []byte) (*InviteDetails, error) {
	var details InviteDetails
	err := r.pool.QueryRow(ctx, `
		SELECT w.name, i.email::text, i.role, i.expires_at
		FROM workspace_invites i
		JOIN workspaces w ON w.id = i.workspace_id
		WHERE i.token_hash = $1 AND i.accepted_at IS NULL
	`, tokenHash).Scan(&details.WorkspaceName, &details.Email, &details.Role, &details.ExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: load invite details: %w", err)
	}
	return &details, nil
}

// lockedInvite carries what RedeemInvite needs, with a row lock held for the
// duration of its transaction so two simultaneous redemptions of the same
// invite cannot both succeed.
type lockedInvite struct {
	ID          string
	WorkspaceID string
	Email       string
	Role        string
	ExpiresAt   time.Time
}

func (r *repository) lockInviteByTokenHash(ctx context.Context, tx pgx.Tx, tokenHash []byte) (*lockedInvite, error) {
	var invite lockedInvite
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, email::text, role, expires_at
		FROM workspace_invites
		WHERE token_hash = $1 AND accepted_at IS NULL
		FOR UPDATE
	`, tokenHash).Scan(&invite.ID, &invite.WorkspaceID, &invite.Email, &invite.Role, &invite.ExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: lock invite: %w", err)
	}
	return &invite, nil
}

func (r *repository) markInviteAccepted(ctx context.Context, tx pgx.Tx, inviteID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE workspace_invites SET accepted_at = now() WHERE id = $1`, inviteID)
	if err != nil {
		return fmt.Errorf("workspace: mark invite accepted: %w", err)
	}
	return nil
}

// insertMemberWithRole is insertOwnerMember's general form: any built-in role,
// not only 'owner'. Bootstrap keeps its own narrower helper because a
// workspace's first member is always its owner by construction, and that
// invariant is clearer written directly than expressed as a call with 'owner'
// threaded through as a parameter.
func (r *repository) insertMemberWithRole(ctx context.Context, tx pgx.Tx, id, workspaceID, userID, role string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO workspace_members (id, workspace_id, user_id, role, presence)
		VALUES ($1, $2, $3, $4, 'offline')
	`, id, workspaceID, userID, role)
	if err != nil {
		return fmt.Errorf("workspace: insert member: %w", err)
	}
	return nil
}
