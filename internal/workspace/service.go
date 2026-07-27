// Package workspace owns tenants, members, teams, invitations, and workspace
// settings.
//
// # Responsibilities
//
// Creation, membership lifecycle, role assignment, branding, locale, and
// retention policy.
//
// # Boundary
//
// The only module permitted to create or delete a workspace row. Everything
// else takes a workspace id as given.
package workspace

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrInvalidSlug = errors.New("workspace: slug must be lowercase letters, numbers, and hyphens")
	ErrInvalidName = errors.New("workspace: name is required")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

type Service struct {
	repo *repository
	pool *database.Pool
}

func New(pool *database.Pool) *Service {
	return &Service{repo: &repository{pool: pool}, pool: pool}
}

// Bootstrap creates a workspace, makes userID its owner, and provisions a
// default inbox — the three things §7.2's onboarding flow needs atomically.
// If any step fails the whole thing rolls back rather than leaving an
// ownerless workspace or an inbox-less one behind.
func (s *Service) Bootstrap(ctx context.Context, userID, name, slug string) (*Workspace, error) {
	name = strings.TrimSpace(name)
	slug = strings.ToLower(strings.TrimSpace(slug))

	if name == "" {
		return nil, ErrInvalidName
	}
	if !slugPattern.MatchString(slug) {
		return nil, ErrInvalidSlug
	}

	workspaceID := ids.New(ids.PrefixWorkspace)

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.insertWorkspace(ctx, tx, workspaceID, name, slug, "SUP"); err != nil {
			return err
		}
		if err := s.repo.insertOwnerMember(ctx, tx, ids.New(ids.PrefixMember), workspaceID, userID); err != nil {
			return err
		}
		return s.repo.insertDefaultInbox(ctx, tx, ids.New(ids.PrefixInbox), workspaceID)
	})
	if err != nil {
		return nil, err
	}

	return s.repo.byID(ctx, workspaceID)
}

// Get returns a workspace by id.
func (s *Service) Get(ctx context.Context, id string) (*Workspace, error) {
	return s.repo.byID(ctx, id)
}

// ListInboxes returns every inbox in workspaceID, default first. Kept on
// Service rather than a new inbox.Service for now: inboxes are provisioned by
// Bootstrap here and internal/inbox has no service layer of its own yet (see
// its doc.go) — this method will move there the day it does, which is a
// straightforward cut since nothing outside this file touches the inboxes
// table directly.
func (s *Service) ListInboxes(ctx context.Context, workspaceID string) ([]Inbox, error) {
	return s.repo.listInboxes(ctx, workspaceID)
}

// DefaultWorkspaceID resolves which workspace a request belongs to when the
// caller did not name one explicitly (see httpserver's workspace-resolution
// middleware) — the first workspace the user belongs to.
func (s *Service) DefaultWorkspaceID(ctx context.Context, userID string) (string, error) {
	return s.repo.firstMembershipWorkspaceID(ctx, userID)
}

// ActorForUser builds the authorization.Actor for userID inside workspaceID:
// their membership row, role, and the effective capability set (seeded role
// permissions unioned with any per-member grants).
//
// This is the one place a role turns into a set of capabilities, which is
// what keeps that mapping out of every handler that needs to check one.
func (s *Service) ActorForUser(ctx context.Context, workspaceID, userID string) (*authorization.Actor, error) {
	member, err := s.repo.memberForUser(ctx, workspaceID, userID)
	if err != nil {
		return nil, err
	}

	caps, err := s.repo.capabilitiesForRole(ctx, member.Role)
	if err != nil {
		return nil, err
	}

	return &authorization.Actor{
		UserID:       userID,
		MemberID:     member.ID,
		WorkspaceID:  workspaceID,
		Role:         member.Role,
		Capabilities: caps,
	}, nil
}
