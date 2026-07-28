package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database"
)

var ErrNotFound = errors.New("workspace: not found")
var ErrSlugTaken = errors.New("workspace: slug already in use")

type Workspace struct {
	ID              string
	Name            string
	Slug            string
	DefaultLanguage string
	Timezone        string
	TicketPrefix    string
	CreatedAt       time.Time
}

type Member struct {
	ID          string
	WorkspaceID string
	UserID      string
	Role        string
	// ExtraCapabilities are grants beyond the role's defaults (§5.9). The
	// effective set is role_permissions ∪ ExtraCapabilities — see
	// ActorForUser, which is the one place that union actually happens.
	ExtraCapabilities []string
	CreatedAt         time.Time
}

type repository struct {
	pool *database.Pool
}

func (r *repository) insertWorkspace(
	ctx context.Context, tx pgx.Tx, id, name, slug, ticketPrefix string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO workspaces (id, name, slug, ticket_prefix)
		VALUES ($1, $2, $3, $4)
	`, id, name, slug, ticketPrefix)
	if err != nil && isUniqueViolation(err) {
		return ErrSlugTaken
	}
	return err
}

// allocateTicketNumber increments the workspace's ticket_sequence and returns
// the prefix and new number in one statement. UPDATE...RETURNING takes the
// row's lock itself, so two tickets created concurrently in the same
// workspace can never be handed the same number — the second writer simply
// waits for the first's transaction to commit or roll back.
func (r *repository) allocateTicketNumber(ctx context.Context, tx pgx.Tx, workspaceID string) (prefix string, number int, err error) {
	err = tx.QueryRow(ctx, `
		UPDATE workspaces SET ticket_sequence = ticket_sequence + 1
		WHERE id = $1
		RETURNING ticket_prefix, ticket_sequence
	`, workspaceID).Scan(&prefix, &number)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	return prefix, number, err
}

func (r *repository) insertOwnerMember(ctx context.Context, tx pgx.Tx, id, workspaceID, userID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO workspace_members (id, workspace_id, user_id, role, presence)
		VALUES ($1, $2, $3, 'owner', 'online')
	`, id, workspaceID, userID)
	return err
}

func (r *repository) insertDefaultInbox(ctx context.Context, tx pgx.Tx, id, workspaceID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO inboxes (id, workspace_id, name, slug, channels, is_default)
		VALUES ($1, $2, 'Support', 'support', ARRAY['widget','portal','email'], true)
	`, id, workspaceID)
	return err
}

// Inbox is the subset of the inboxes row exposed today. Full inbox management
// (§6.1) — creating additional inboxes, editing channels, team access — is
// internal/inbox's territory once that module has a service layer; this
// method exists only so a caller can discover the id Bootstrap already
// created, without reaching into the database directly.
type Inbox struct {
	ID        string
	Name      string
	Slug      string
	IsDefault bool
}

func (r *repository) listInboxes(ctx context.Context, workspaceID string) ([]Inbox, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, slug, is_default
		FROM inboxes
		WHERE workspace_id = $1
		ORDER BY is_default DESC, name ASC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Inbox
	for rows.Next() {
		var i Inbox
		if err := rows.Scan(&i.ID, &i.Name, &i.Slug, &i.IsDefault); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *repository) byID(ctx context.Context, id string) (*Workspace, error) {
	var w Workspace
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, slug, default_language, timezone, ticket_prefix, created_at
		FROM workspaces WHERE id = $1
	`, id).Scan(&w.ID, &w.Name, &w.Slug, &w.DefaultLanguage, &w.Timezone, &w.TicketPrefix, &w.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// memberForUser returns the caller's membership row for workspaceID, used
// both to build the request Actor and to answer "which workspaces am I in".
func (r *repository) memberForUser(ctx context.Context, workspaceID, userID string) (*Member, error) {
	var m Member
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, user_id, role, extra_capabilities, created_at
		FROM workspace_members
		WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID).Scan(
		&m.ID, &m.WorkspaceID, &m.UserID, &m.Role, &m.ExtraCapabilities, &m.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// firstMembershipWorkspaceID picks a default workspace for a user with
// exactly one (the common case just after setup). Ordered by creation so the
// result is stable across calls.
func (r *repository) firstMembershipWorkspaceID(ctx context.Context, userID string) (string, error) {
	var workspaceID string
	err := r.pool.QueryRow(ctx, `
		SELECT workspace_id FROM workspace_members
		WHERE user_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, userID).Scan(&workspaceID)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return workspaceID, err
}

// capabilitiesForRole loads the seeded role_permissions for a built-in role
// key (§0001 migration). Extra per-member grants are unioned in by the
// caller — kept separate here because role permissions rarely change and are
// worth a cheap in-process cache later; member grants never are.
func (r *repository) capabilitiesForRole(ctx context.Context, roleKey string) (map[authorization.Capability]bool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT rp.capability
		FROM role_permissions rp
		JOIN roles r ON r.id = rp.role_id
		WHERE r.key = $1 AND r.workspace_id IS NULL
	`, roleKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	caps := make(map[authorization.Capability]bool)
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			return nil, err
		}
		caps[authorization.Capability(capability)] = true
	}
	return caps, rows.Err()
}

// RoleDefinition is a built-in role and the capabilities it grants — the
// read-only matrix the Roles settings screen renders (§5.9). Custom roles
// are out of scope until a later release; every workspace shares this same
// fixed set for now.
type RoleDefinition struct {
	Key          string
	Name         string
	Description  *string
	Capabilities []string
}

// listRoleDefinitions loads every built-in role and its seeded permissions in
// one query, ordered so owner (which Actor.Can short-circuits rather than
// storing permissions for) sorts first regardless of insertion order.
func (r *repository) listRoleDefinitions(ctx context.Context) ([]RoleDefinition, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT r.key, r.name, r.description,
		       coalesce(
		           array_agg(rp.capability) FILTER (WHERE rp.capability IS NOT NULL),
		           '{}'
		       ) AS capabilities
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE r.workspace_id IS NULL AND r.is_builtin
		GROUP BY r.id, r.key, r.name, r.description
		ORDER BY (r.key != 'owner'), r.key
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RoleDefinition{}
	for rows.Next() {
		var role RoleDefinition
		if err := rows.Scan(&role.Key, &role.Name, &role.Description, &role.Capabilities); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

// memberDisplayName resolves a member id to the user's current name, for
// denormalising into an audit entry at write time. Runs inside the caller's
// transaction so it sees the same snapshot everything else in that write does.
func (r *repository) memberDisplayName(ctx context.Context, tx pgx.Tx, memberID string) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `
		SELECT u.name FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.id = $1
	`, memberID).Scan(&name)
	return name, err
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
