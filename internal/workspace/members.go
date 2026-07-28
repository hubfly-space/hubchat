package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
)

var (
	ErrInvalidRole       = errors.New("workspace: not a recognised role")
	ErrLastOwner         = errors.New("workspace: a workspace must always have an owner")
	ErrCannotDemoteOwner = errors.New("workspace: ownership cannot be transferred here")
	ErrMemberNotFound    = errors.New("workspace: member not found")
	ErrSelfRemoval       = errors.New("workspace: use the account settings to leave a workspace")
)

// builtinRoles mirrors the CHECK constraint on workspace_members.role
// (migration 0001). Custom roles are explicitly deferred by the product
// itself (§5.9: "Custom roles can be added after the initial release") — this
// module assigns one of these six and nothing else.
var builtinRoles = map[string]bool{
	"owner": true, "admin": true, "manager": true,
	"agent": true, "developer": true, "analyst": true,
}

// ValidRole reports whether role is one of the built-in roles a member may
// hold. Exported so the HTTP layer can validate before calling SetMemberRole
// and return a clean 422 rather than a generic 500.
func ValidRole(role string) bool { return builtinRoles[role] }

// SetMemberRole changes a member's role.
//
// Two invariants are enforced here, not just at the database's uniqueness
// index (which only prevents a *second* owner, not zero owners):
//
//   - Ownership is never granted through this path. Promoting someone to
//     owner is a transfer of ultimate control, and §5.2 draws a hard line
//     between "administrator" (manages people and settings) and "owner"
//     (can also give the workspace away) precisely so that line is not
//     crossed by the same endpoint that changes an agent to a manager.
//   - The sole owner can never be demoted. Nothing else in the system is
//     positioned to notice a workspace with zero owners until someone tries
//     to do something only an owner can do, by which point the workspace is
//     unrecoverable without direct database access.
func (s *Service) SetMemberRole(ctx context.Context, workspaceID, actorMemberID, targetMemberID, role string) error {
	if !ValidRole(role) {
		return ErrInvalidRole
	}
	if role == "owner" {
		return ErrCannotDemoteOwner
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		target, err := s.repo.lockMember(ctx, tx, workspaceID, targetMemberID)
		if err != nil {
			return err
		}
		if target.Role == "owner" {
			return ErrCannotDemoteOwner
		}

		if err := s.repo.updateMemberRole(ctx, tx, targetMemberID, role); err != nil {
			return err
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID,
			ActorType:   audit.ActorUser,
			ActorID:     actorMemberID,
			Action:      audit.MemberRoleChanged,
			EntityType:  "member",
			EntityID:    targetMemberID,
			Metadata:    map[string]any{"from_role": target.Role, "to_role": role},
		}); err != nil {
			return err
		}

		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.MemberRoleSet,
			EntityType:  "member",
			EntityID:    targetMemberID,
			ActorType:   events.ActorUser,
			ActorID:     actorMemberID,
			Data:        map[string]any{"member_id": targetMemberID, "role": role},
		})
	})
}

// SetExtraCapabilities replaces a member's per-member capability grants
// (§5.9), beyond what their role includes by default.
//
// Replacing the whole set rather than adding/removing one at a time matches
// how the settings screen presents it — a checklist the admin submits in
// full — and avoids a lost-update race between two admins editing the same
// member's grants from two tabs.
func (s *Service) SetExtraCapabilities(
	ctx context.Context, workspaceID, actorMemberID, targetMemberID string, capabilities []authorization.Capability,
) error {
	names := make([]string, len(capabilities))
	for i, capability := range capabilities {
		names[i] = string(capability)
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		target, err := s.repo.lockMember(ctx, tx, workspaceID, targetMemberID)
		if err != nil {
			return err
		}

		if err := s.repo.updateExtraCapabilities(ctx, tx, targetMemberID, names); err != nil {
			return err
		}

		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID,
			ActorType:   audit.ActorUser,
			ActorID:     actorMemberID,
			Action:      audit.MemberRoleChanged,
			EntityType:  "member",
			EntityID:    targetMemberID,
			Metadata:    map[string]any{"role": target.Role, "extra_capabilities": names},
		})
	})
}

// RemoveMember removes a member from the workspace.
//
// A member cannot remove themselves through this path — that is "leaving a
// workspace," a different, self-service action with its own confirmation, not
// something an admin's "remove member" button should also accidentally do to
// its caller. The sole owner can never be removed, for the same reason they
// can never be demoted.
func (s *Service) RemoveMember(ctx context.Context, workspaceID, actorMemberID, targetMemberID string) error {
	if actorMemberID == targetMemberID {
		return ErrSelfRemoval
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		target, err := s.repo.lockMember(ctx, tx, workspaceID, targetMemberID)
		if err != nil {
			return err
		}
		if target.Role == "owner" {
			return ErrLastOwner
		}

		if err := s.repo.deleteMember(ctx, tx, targetMemberID); err != nil {
			return err
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID,
			ActorType:   audit.ActorUser,
			ActorID:     actorMemberID,
			Action:      audit.MemberRemoved,
			EntityType:  "member",
			EntityID:    targetMemberID,
			Metadata:    map[string]any{"role": target.Role},
		}); err != nil {
			return err
		}

		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.MemberRemoved,
			EntityType:  "member",
			EntityID:    targetMemberID,
			ActorType:   events.ActorUser,
			ActorID:     actorMemberID,
			Data:        map[string]any{"member_id": targetMemberID},
		})
	})
}

// SetOwnPresence and SetOwnAcceptingConversations are self-service — a member
// updates their own status without needing member.manage. Scoped by userID
// rather than memberID from the URL specifically so one member can never flip
// another's status by guessing an id.
func (s *Service) SetOwnPresence(ctx context.Context, workspaceID, userID, presence string) error {
	if !validPresence[presence] {
		return ErrInvalidPresence
	}
	return s.repo.updatePresence(ctx, workspaceID, userID, presence)
}

var validPresence = map[string]bool{"online": true, "away": true, "busy": true, "offline": true}

// ErrInvalidPresence is returned for a presence value outside the enum.
var ErrInvalidPresence = errors.New("workspace: not a recognised presence state")

func (s *Service) SetOwnAcceptingConversations(ctx context.Context, workspaceID, userID string, accepting bool) error {
	return s.repo.updateAcceptingConversations(ctx, workspaceID, userID, accepting)
}

// ---------------------------------------------------------------- repository

func (r *repository) lockMember(ctx context.Context, tx pgx.Tx, workspaceID, memberID string) (*Member, error) {
	var m Member
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, user_id, role, extra_capabilities, created_at
		FROM workspace_members
		WHERE workspace_id = $1 AND id = $2
		FOR UPDATE
	`, workspaceID, memberID).Scan(
		&m.ID, &m.WorkspaceID, &m.UserID, &m.Role, &m.ExtraCapabilities, &m.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMemberNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: lock member: %w", err)
	}
	return &m, nil
}

func (r *repository) updateMemberRole(ctx context.Context, tx pgx.Tx, memberID, role string) error {
	_, err := tx.Exec(ctx,
		`UPDATE workspace_members SET role = $2 WHERE id = $1`, memberID, role)
	if err != nil {
		return fmt.Errorf("workspace: update member role: %w", err)
	}
	return nil
}

func (r *repository) updateExtraCapabilities(ctx context.Context, tx pgx.Tx, memberID string, capabilities []string) error {
	_, err := tx.Exec(ctx,
		`UPDATE workspace_members SET extra_capabilities = $2 WHERE id = $1`, memberID, capabilities)
	if err != nil {
		return fmt.Errorf("workspace: update extra capabilities: %w", err)
	}
	return nil
}

func (r *repository) deleteMember(ctx context.Context, tx pgx.Tx, memberID string) error {
	_, err := tx.Exec(ctx, `DELETE FROM workspace_members WHERE id = $1`, memberID)
	if err != nil {
		return fmt.Errorf("workspace: delete member: %w", err)
	}
	return nil
}

func (r *repository) updatePresence(ctx context.Context, workspaceID, userID, presence string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE workspace_members SET presence = $3, last_seen_at = now()
		WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID, presence)
	if err != nil {
		return fmt.Errorf("workspace: update presence: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	return nil
}

func (r *repository) updateAcceptingConversations(ctx context.Context, workspaceID, userID string, accepting bool) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE workspace_members SET accepting_conversations = $3
		WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID, accepting)
	if err != nil {
		return fmt.Errorf("workspace: update accepting_conversations: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	return nil
}
