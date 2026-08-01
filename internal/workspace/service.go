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

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrInvalidSlug = errors.New("workspace: slug must be lowercase letters, numbers, and hyphens")
	ErrInvalidName = errors.New("workspace: name is required")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

type Service struct {
	repo   *repository
	pool   *database.Pool
	events *events.Log
	audit  *audit.Log
}

// New constructs the workspace service. eventLog and auditLog may be nil in
// tests that do not exercise publication — every write site nil-checks
// through appendEvent and recordAudit.
func New(pool *database.Pool, eventLog *events.Log, auditLog *audit.Log) *Service {
	return &Service{repo: &repository{pool: pool}, pool: pool, events: eventLog, audit: auditLog}
}

// appendEvent records a state change on the workspace event log inside the
// caller's transaction. See conversation.Service.appendEvent for why this
// exists instead of a direct call to realtime — the short version is that an
// event is durable and multi-consumer where a broadcast is neither.
func (s *Service) appendEvent(ctx context.Context, tx pgx.Tx, event events.Event) error {
	if s.events == nil {
		return nil
	}
	_, err := s.events.Append(ctx, tx, event)
	return err
}

// recordAudit writes an audit entry inside the caller's transaction.
//
// Membership, role, and invite changes are exactly the security-relevant
// actions §6.19 asks for: who was promoted, who was removed, who issued an
// invite. Writing the entry in the same transaction as the change means the
// audit trail cannot show an action that the database says never committed.
//
// Every call site in this package passes ActorID as a member id and leaves
// ActorName unset — resolving it once here, rather than in each of them,
// is what actually gets the name denormalised into the row. Skipping this
// would leave every entry blank on the one field audit.go's own doc comment
// says the whole design exists for: reading correctly after the member is
// gone and there is nothing left to join against.
func (s *Service) recordAudit(ctx context.Context, tx pgx.Tx, entry audit.Entry) error {
	if s.audit == nil {
		return nil
	}
	if entry.ActorName == "" && entry.ActorType == audit.ActorUser && entry.ActorID != "" {
		if name, err := s.repo.memberDisplayName(ctx, tx, entry.ActorID); err == nil {
			entry.ActorName = name
		}
		// A lookup failure is not fatal to the audit write: the action still
		// matters even if the actor's current name could not be resolved (a
		// row lock conflict, a already-removed member mid-transaction), and
		// refusing to record who did *something* over a missing *name* would
		// be the wrong trade.
	}
	return audit.RecordTx(ctx, tx, entry)
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

// AllocateTicketNumber hands the ticket module the next display number for
// workspaceID, inside the caller's own transaction — so a ticket insert and
// its number allocation commit or roll back together, and a failed create
// never leaves a gap another module has to explain (§6.3 ticket numbering).
func (s *Service) AllocateTicketNumber(ctx context.Context, tx pgx.Tx, workspaceID string) (prefix string, number int, err error) {
	return s.repo.allocateTicketNumber(ctx, tx, workspaceID)
}

// ListRoles returns the built-in catalog plus the current workspace's custom
// roles. Owner's row is filled in from authorization.AllCapabilityNames rather
// than role_permissions, mirroring how Actor.Can short-circuits owner.
func (s *Service) ListRoles(ctx context.Context, workspaceID string) ([]RoleDefinition, error) {
	return s.ListRolesPage(ctx, workspaceID, RoleListCursor{}, 10000)
}

// ListRolesPage returns the built-in catalog plus one workspace's custom
// roles in the same stable order as ListRoles, bounded by limit+1 for the API
// cursor envelope.
func (s *Service) ListRolesPage(ctx context.Context, workspaceID string, before RoleListCursor, limit int) ([]RoleDefinition, error) {
	roles, err := s.repo.listRoleDefinitionsPage(ctx, workspaceID, before, limit)
	if err != nil {
		return nil, err
	}
	for i := range roles {
		if roles[i].Key == "owner" {
			roles[i].Capabilities = authorization.AllCapabilityNames()
		}
	}
	return roles, nil
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

	caps, err := s.repo.capabilitiesForRole(ctx, workspaceID, member.Role)
	if err != nil {
		return nil, err
	}

	// The union the repository's own comment promises. Owner short-circuits
	// in Actor.Can regardless, so this only matters for every other role —
	// which is exactly the grant this is for (§5.9: capabilities beyond a
	// role's defaults).
	for _, capability := range member.ExtraCapabilities {
		caps[authorization.Capability(capability)] = true
	}

	return &authorization.Actor{
		UserID:       userID,
		MemberID:     member.ID,
		WorkspaceID:  workspaceID,
		Role:         member.Role,
		Capabilities: caps,
	}, nil
}
