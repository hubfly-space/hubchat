package workspace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

// inviteTokenLifetime is generous relative to the auth module's other tokens:
// an invite is often forwarded, discussed, and acted on days later, not in the
// same sitting it was sent.
const inviteTokenLifetime = 7 * 24 * time.Hour

var (
	ErrInviteExists        = errors.New("workspace: this address already has a pending invite")
	ErrAlreadyMember       = errors.New("workspace: this address already belongs to the workspace")
	ErrInviteNotFound      = errors.New("workspace: invite not found")
	ErrInviteExpired       = errors.New("workspace: this invite has expired")
	ErrInviteEmailMismatch = errors.New("workspace: this invite was issued to a different address")
)

// Invite is a pending or historical workspace invitation.
type Invite struct {
	ID         string
	Email      string
	Role       string
	InvitedBy  *string
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	CreatedAt  time.Time
}

// InviteDetails is what an unauthenticated visitor holding an invite link may
// see before accepting — enough to render "You've been invited to join X as
// Y", nothing about the workspace beyond that.
type InviteDetails struct {
	WorkspaceName string
	Email         string
	Role          string
	ExpiresAt     time.Time
}

// IssueInvite creates a pending invite and returns the raw token to email.
//
// Refuses an email that is already a member (there is nothing to invite) or
// already has a pending invite (the unique index on (workspace_id, email)
// would refuse the insert anyway; checking first gives a clearer error than a
// generic conflict). The raw token exists only in this return value and in
// the email the caller sends — the database keeps only its hash (§11.5).
func (s *Service) IssueInvite(ctx context.Context, workspaceID, actorMemberID, email, role string) (token string, invite *Invite, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	role = strings.TrimSpace(role)
	if err := s.validateAssignableRole(ctx, workspaceID, role); err != nil {
		return "", nil, err
	}

	if isMember, err := s.repo.emailIsMember(ctx, workspaceID, email); err != nil {
		return "", nil, err
	} else if isMember {
		return "", nil, ErrAlreadyMember
	}

	rawToken, err := newInviteToken()
	if err != nil {
		return "", nil, err
	}

	id := ids.New(ids.PrefixInvite)
	expiresAt := time.Now().Add(inviteTokenLifetime)

	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.insertInvite(ctx, tx, id, workspaceID, email, role, actorMemberID, hashInviteToken(rawToken), expiresAt); err != nil {
			return err
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID,
			ActorType:   audit.ActorUser,
			ActorID:     actorMemberID,
			Action:      audit.MemberInvited,
			EntityType:  "invite",
			EntityID:    id,
			Metadata:    map[string]any{"email": email, "role": role},
		}); err != nil {
			return err
		}

		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.InviteIssued,
			EntityType:  "invite",
			EntityID:    id,
			ActorType:   events.ActorUser,
			ActorID:     actorMemberID,
			Data:        map[string]any{"invite_id": id, "email": email, "role": role},
		})
	})
	if err != nil {
		if errors.Is(err, errUniqueInvite) {
			return "", nil, ErrInviteExists
		}
		return "", nil, err
	}

	invite, err = s.repo.inviteByID(ctx, workspaceID, id)
	return rawToken, invite, err
}

// ListInvites returns pending and historical invites for a workspace.
func (s *Service) ListInvites(ctx context.Context, workspaceID string) ([]Invite, error) {
	return s.repo.listInvites(ctx, workspaceID)
}

// ListInvitesPage returns the invite history newest first with a timestamp/id
// cursor so large workspaces do not need to materialize every invitation.
func (s *Service) ListInvitesPage(ctx context.Context, workspaceID string, before time.Time, beforeID string, limit int) ([]Invite, error) {
	return s.repo.listInvitesPage(ctx, workspaceID, before, beforeID, limit)
}

// RevokeInvite cancels a pending invite. Idempotent against one already
// accepted or expired — revoking is a no-op, not an error, because the
// outcome the caller wants ("this invite must not work") already holds.
func (s *Service) RevokeInvite(ctx context.Context, workspaceID, actorMemberID, inviteID string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.deleteInvite(ctx, tx, workspaceID, inviteID); err != nil {
			return err
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID,
			ActorType:   audit.ActorUser,
			ActorID:     actorMemberID,
			Action:      audit.MemberInvited, // same action family; the metadata says which
			EntityType:  "invite",
			EntityID:    inviteID,
			Metadata:    map[string]any{"revoked": true},
		}); err != nil {
			return err
		}

		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID,
			Type:        events.InviteRevoked,
			EntityType:  "invite",
			EntityID:    inviteID,
			ActorType:   events.ActorUser,
			ActorID:     actorMemberID,
		})
	})
}

// LookupInvite resolves a raw token to its public details, for the
// unauthenticated accept-invite page. Does not require the caller to be
// signed in — that is the whole point of an invite link.
func (s *Service) LookupInvite(ctx context.Context, token string) (*InviteDetails, error) {
	return s.repo.inviteDetailsByTokenHash(ctx, hashInviteToken(token))
}

// RedeemInvite accepts an invite on behalf of userID.
//
// userID's account email must match the invite's exactly — case-insensitively,
// since citext already normalises storage but the comparison here is against
// whatever the caller resolved userID's email to. This is what stops someone
// from forwarding an invite meant for one address and redeeming it under a
// different account: the invite names a specific person to add, not a bearer
// token for "someone gets a seat."
func (s *Service) RedeemInvite(ctx context.Context, token, userID, userEmail string) (*Workspace, error) {
	tokenHash := hashInviteToken(token)

	var workspace *Workspace
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		invite, err := s.repo.lockInviteByTokenHash(ctx, tx, tokenHash)
		if err != nil {
			return err
		}
		if time.Now().After(invite.ExpiresAt) {
			return ErrInviteExpired
		}
		if !strings.EqualFold(invite.Email, userEmail) {
			return ErrInviteEmailMismatch
		}

		memberID := ids.New(ids.PrefixMember)
		if err := s.repo.insertMemberWithRole(ctx, tx, memberID, invite.WorkspaceID, userID, invite.Role); err != nil {
			return err
		}
		if err := s.repo.markInviteAccepted(ctx, tx, invite.ID); err != nil {
			return err
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: invite.WorkspaceID,
			ActorType:   audit.ActorUser,
			ActorID:     userID,
			Action:      audit.MemberJoined,
			EntityType:  "member",
			EntityID:    memberID,
			Metadata:    map[string]any{"role": invite.Role, "via_invite": invite.ID},
		}); err != nil {
			return err
		}

		if err := s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: invite.WorkspaceID,
			Type:        events.InviteAccepted,
			EntityType:  "invite",
			EntityID:    invite.ID,
			ActorType:   events.ActorUser,
			ActorID:     userID,
		}); err != nil {
			return err
		}
		if err := s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: invite.WorkspaceID,
			Type:        events.MemberJoined,
			EntityType:  "member",
			EntityID:    memberID,
			ActorType:   events.ActorUser,
			ActorID:     userID,
			Data:        map[string]any{"member_id": memberID, "role": invite.Role},
		}); err != nil {
			return err
		}

		workspace, err = s.repo.byID(ctx, invite.WorkspaceID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return workspace, nil
}

// newInviteToken generates a raw token with the same shape as the auth
// module's session and reset tokens (32 bytes, base64url) — there is no
// reason an invite link should look different from any other capability URL
// in the product, and workspace does not import auth for it to stay
// self-contained per its own module boundary.
func newInviteToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashInviteToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
