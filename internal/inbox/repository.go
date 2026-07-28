package inbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hubchat/hubchat/internal/database"
)

var (
	ErrNotFound        = errors.New("inbox: not found")
	ErrInvalidName     = errors.New("inbox: name is required")
	ErrInvalidSlug     = errors.New("inbox: slug is required")
	ErrSlugTaken       = errors.New("inbox: a workspace inbox already uses this slug")
	ErrInvalidChannel  = errors.New("inbox: not a recognised channel")
	ErrLastInbox       = errors.New("inbox: a workspace must keep at least one inbox")
	ErrHasConversations = errors.New("inbox: this inbox still has conversations; reassign them first")
)

// validChannels mirrors the CHECK constraint on conversations.channel
// (migration 0002) — inboxes.channels is unconstrained at the database level
// only because an array column cannot carry a CHECK per element, not because
// any value is actually acceptable.
var validChannels = map[string]bool{
	"widget": true, "portal": true, "email": true, "form": true, "api": true, "manual": true,
}

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

type repository struct {
	pool *database.Pool
}

var errUniqueSlug = errors.New("inbox: duplicate slug")

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func (r *repository) insert(
	ctx context.Context, tx pgx.Tx, id, workspaceID, name, slug string, description *string, channels []string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO inboxes (id, workspace_id, name, slug, description, channels)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, workspaceID, name, slug, description, channels)
	if err != nil {
		if isUniqueViolation(err) {
			return errUniqueSlug
		}
		return fmt.Errorf("inbox: insert: %w", err)
	}
	return nil
}

func (r *repository) update(
	ctx context.Context, tx pgx.Tx, workspaceID, id, name string, description *string, channels []string,
) error {
	tag, err := tx.Exec(ctx, `
		UPDATE inboxes SET name = $3, description = $4, channels = $5
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, name, description, channels)
	if err != nil {
		return fmt.Errorf("inbox: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) setTeams(ctx context.Context, tx pgx.Tx, id string, teamIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM inbox_teams WHERE inbox_id = $1`, id); err != nil {
		return fmt.Errorf("inbox: clear teams: %w", err)
	}
	for _, teamID := range teamIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO inbox_teams (inbox_id, team_id) VALUES ($1, $2)
		`, id, teamID); err != nil {
			return fmt.Errorf("inbox: assign team: %w", err)
		}
	}
	return nil
}

func (r *repository) setDefault(ctx context.Context, tx pgx.Tx, workspaceID, id string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE inboxes SET is_default = false WHERE workspace_id = $1
	`, workspaceID); err != nil {
		return fmt.Errorf("inbox: clear default: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE inboxes SET is_default = true WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("inbox: set default: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) delete(ctx context.Context, tx pgx.Tx, workspaceID, id string) error {
	var isDefault bool
	if err := tx.QueryRow(ctx, `
		SELECT is_default FROM inboxes WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id).Scan(&isDefault); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("inbox: load for delete: %w", err)
	}
	if isDefault {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM inboxes WHERE workspace_id = $1`, workspaceID).Scan(&count); err != nil {
			return fmt.Errorf("inbox: count: %w", err)
		}
		if count <= 1 {
			return ErrLastInbox
		}
	}

	tag, err := tx.Exec(ctx, `DELETE FROM inboxes WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return ErrHasConversations
		}
		return fmt.Errorf("inbox: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if isDefault {
		if _, err := tx.Exec(ctx, `
			UPDATE inboxes SET is_default = true
			WHERE id = (SELECT id FROM inboxes WHERE workspace_id = $1 ORDER BY created_at LIMIT 1)
		`, workspaceID); err != nil {
			return fmt.Errorf("inbox: promote new default: %w", err)
		}
	}
	return nil
}

// selectInbox is the query byID/list share, differing only by predicate.
// open_count comes from the same partial condition as conversations_active_queue
// (migration 0002) so this number always matches what the inbox's own list
// screen shows.
const selectInbox = `
	SELECT i.id, i.workspace_id, i.name, i.slug::text, i.description, i.channels,
	       i.is_default, i.sla_policy_id, i.created_at,
	       coalesce(array_agg(it.team_id) FILTER (WHERE it.team_id IS NOT NULL), '{}'),
	       (SELECT count(*) FROM conversations c
	        WHERE c.inbox_id = i.id AND c.state NOT IN ('closed', 'spam'))
	FROM inboxes i
	LEFT JOIN inbox_teams it ON it.inbox_id = i.id
`

func scanInbox(row interface {
	Scan(dest ...any) error
}) (*Inbox, error) {
	var i Inbox
	err := row.Scan(
		&i.ID, &i.WorkspaceID, &i.Name, &i.Slug, &i.Description, &i.Channels,
		&i.IsDefault, &i.SLAPolicyID, &i.CreatedAt, &i.TeamIDs, &i.OpenCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *repository) byID(ctx context.Context, workspaceID, id string) (*Inbox, error) {
	row := r.pool.QueryRow(ctx, selectInbox+`
		WHERE i.workspace_id = $1 AND i.id = $2
		GROUP BY i.id
	`, workspaceID, id)
	return scanInbox(row)
}

// memberDisplayName resolves a member id to the user's current name, for
// denormalising into an audit entry at write time (mirrors
// internal/workspace's identical query — see that package for why this is
// duplicated rather than shared).
func (r *repository) memberDisplayName(ctx context.Context, tx pgx.Tx, memberID string) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `
		SELECT u.name FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.id = $1
	`, memberID).Scan(&name)
	return name, err
}

func (r *repository) list(ctx context.Context, workspaceID string) ([]Inbox, error) {
	rows, err := r.pool.Query(ctx, selectInbox+`
		WHERE i.workspace_id = $1
		GROUP BY i.id
		ORDER BY i.is_default DESC, i.name ASC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("inbox: list: %w", err)
	}
	defer rows.Close()

	out := []Inbox{}
	for rows.Next() {
		i, err := scanInbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}
