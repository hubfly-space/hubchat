package workspace

import (
	"context"
	"errors"
	"fmt"
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
	ID             string
	WorkspaceID    string
	UserID         string
	Role           string
	ScimExternalID *string
	DeactivatedAt  *time.Time
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

// Inbox is what the dashboard's opening payload carries for one inbox. Full
// inbox management (§6.1) — creating additional inboxes, editing channels,
// team access — is internal/inbox's territory; this method exists only so
// Bootstrap can serve the Overview's inbox section without a second request.
// The query mirrors internal/inbox's selectInbox, so the number this payload
// reports for open volume always matches what the inbox's own list screen
// shows.
type Inbox struct {
	ID          string
	WorkspaceID string
	Name        string
	Slug        string
	Description *string
	Channels    []string
	TeamIDs     []string
	IsDefault   bool
	SLAPolicyID *string
	OpenCount   int
	CreatedAt   time.Time
}

func (r *repository) listInboxes(ctx context.Context, workspaceID string) ([]Inbox, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT i.id, i.workspace_id, i.name, i.slug::text, i.description, i.channels,
		       i.is_default, i.sla_policy_id, i.created_at,
		       coalesce(array_agg(it.team_id) FILTER (WHERE it.team_id IS NOT NULL), '{}'),
		       (SELECT count(*) FROM conversations c
		        WHERE c.inbox_id = i.id AND c.state NOT IN ('closed', 'spam'))
		FROM inboxes i
		LEFT JOIN inbox_teams it ON it.inbox_id = i.id
		WHERE i.workspace_id = $1
		GROUP BY i.id
		ORDER BY i.is_default DESC, i.name ASC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Inbox
	for rows.Next() {
		var i Inbox
		if err := rows.Scan(
			&i.ID, &i.WorkspaceID, &i.Name, &i.Slug, &i.Description, &i.Channels,
			&i.IsDefault, &i.SLAPolicyID, &i.CreatedAt, &i.TeamIDs, &i.OpenCount,
		); err != nil {
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
		SELECT id, workspace_id, user_id, role, extra_capabilities, scim_external_id, deactivated_at, created_at
		FROM workspace_members
		WHERE workspace_id = $1 AND user_id = $2 AND deactivated_at IS NULL
	`, workspaceID, userID).Scan(
		&m.ID, &m.WorkspaceID, &m.UserID, &m.Role, &m.ExtraCapabilities, &m.ScimExternalID, &m.DeactivatedAt, &m.CreatedAt,
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
		WHERE user_id = $1 AND deactivated_at IS NULL
		ORDER BY created_at ASC
		LIMIT 1
	`, userID).Scan(&workspaceID)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return workspaceID, err
}

// capabilitiesForRole loads the workspace role first, falling back to the
// global built-in catalog. Extra per-member grants are unioned in by the
// caller — kept separate because role permissions and member grants have
// different ownership and audit paths.
func (r *repository) capabilitiesForRole(ctx context.Context, workspaceID, roleKey string) (map[authorization.Capability]bool, error) {
	var roleID string
	var capabilities []string
	err := r.pool.QueryRow(ctx, `
		SELECT r.id, coalesce(array_agg(rp.capability) FILTER (WHERE rp.capability IS NOT NULL), '{}')
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE r.key = $2 AND (r.workspace_id = $1 OR (r.workspace_id IS NULL AND r.is_builtin))
		GROUP BY r.id, r.workspace_id
		ORDER BY (r.workspace_id IS NULL)
		LIMIT 1
	`, workspaceID, roleKey).Scan(&roleID, &capabilities)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, err
	}

	caps := make(map[authorization.Capability]bool, len(capabilities))
	for _, capability := range capabilities {
		caps[authorization.Capability(capability)] = true
	}
	return caps, nil
}

// RoleDefinition is a built-in or workspace-owned role and its capability set.
type RoleDefinition struct {
	ID           string
	WorkspaceID  string
	Key          string
	Name         string
	Description  *string
	IsBuiltin    bool
	Capabilities []string
}

// listRoleDefinitionsPage loads the global presets and one workspace's
// custom roles in one bounded query. The rank bits sort built-ins and owner
// first; name/key are ascending tie-breakers.
func (r *repository) listRoleDefinitionsPage(ctx context.Context, workspaceID string, before RoleListCursor, limit int) ([]RoleDefinition, error) {
	if limit <= 0 || limit > 10000 {
		limit = 50
	}
	args := []any{workspaceID}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	where := "((r.workspace_id IS NULL AND r.is_builtin) OR r.workspace_id = $1)"
	if before.Key != "" {
		builtin := arg(before.BuiltinRank)
		owner := arg(before.OwnerRank)
		name := arg(before.Name)
		key := arg(before.Key)
		where += ` AND (
			(CASE WHEN r.is_builtin THEN 1 ELSE 0 END) < ` + builtin + `
			OR ((CASE WHEN r.is_builtin THEN 1 ELSE 0 END) = ` + builtin + ` AND (
				(CASE WHEN r.key = 'owner' THEN 1 ELSE 0 END) < ` + owner + `
				OR ((CASE WHEN r.key = 'owner' THEN 1 ELSE 0 END) = ` + owner + ` AND (
					r.name > ` + name + ` OR (r.name = ` + name + ` AND r.key > ` + key + `)
				))
			))
		)`
	}
	limitPlaceholder := arg(limit)
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, coalesce(r.workspace_id,''), r.key, r.name, r.description, r.is_builtin,
		       coalesce(
		           array_agg(rp.capability) FILTER (WHERE rp.capability IS NOT NULL),
		           '{}'
	       ) AS capabilities
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE `+where+`
		GROUP BY r.id, r.workspace_id, r.key, r.name, r.description, r.is_builtin
		ORDER BY r.is_builtin DESC, (r.key = 'owner') DESC, r.name, r.key
		LIMIT `+limitPlaceholder, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RoleDefinition{}
	for rows.Next() {
		var role RoleDefinition
		if err := rows.Scan(&role.ID, &role.WorkspaceID, &role.Key, &role.Name, &role.Description, &role.IsBuiltin, &role.Capabilities); err != nil {
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
