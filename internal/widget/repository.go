package widget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
)

type repository struct {
	pool *database.Pool
}

const widgetColumns = `
	id, workspace_id, name, public_key, inbox_id, modes, appearance, content, behavior,
	environment, rollout_percent, version, enabled, installed_at, last_seen_at, created_at, updated_at
`

func scanWidget(row interface{ Scan(dest ...any) error }) (*Widget, error) {
	var w Widget
	err := row.Scan(
		&w.ID, &w.WorkspaceID, &w.Name, &w.PublicKey, &w.InboxID, &w.Modes,
		&w.Appearance, &w.Content, &w.Behavior, &w.Environment, &w.RolloutPercent,
		&w.Version, &w.Enabled, &w.InstalledAt, &w.LastSeenAt, &w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *repository) list(ctx context.Context, workspaceID string) ([]Widget, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+widgetColumns+`
		FROM widgets WHERE workspace_id = $1 ORDER BY created_at
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("widget: list: %w", err)
	}
	defer rows.Close()

	out := []Widget{}
	for rows.Next() {
		w, err := scanWidget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

func (r *repository) byID(ctx context.Context, workspaceID, id string) (*Widget, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+widgetColumns+`
		FROM widgets WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	return scanWidget(row)
}

func (r *repository) byIDTx(ctx context.Context, tx pgx.Tx, workspaceID, id string) (*Widget, error) {
	row := tx.QueryRow(ctx, `SELECT `+widgetColumns+`
		FROM widgets WHERE workspace_id = $1 AND id = $2 FOR UPDATE
	`, workspaceID, id)
	return scanWidget(row)
}

// byPublicKey is the unauthenticated lookup the loader's config request
// uses. Not workspace-scoped by anything the caller supplies — the public
// key itself is what names the workspace, since a visitor's browser has no
// other credential yet.
func (r *repository) byPublicKey(ctx context.Context, publicKey string) (*Widget, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+widgetColumns+`
		FROM widgets WHERE public_key = $1
	`, publicKey)
	return scanWidget(row)
}

func (r *repository) insert(
	ctx context.Context, tx pgx.Tx,
	id, workspaceID, name, publicKey string, inboxID *string,
	modes []string, appearance, content, behavior map[string]any,
) (*Widget, error) {
	appearanceJSON, err := json.Marshal(appearance)
	if err != nil {
		return nil, err
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	behaviorJSON, err := json.Marshal(behavior)
	if err != nil {
		return nil, err
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO widgets (id, workspace_id, name, public_key, inbox_id, modes, appearance, content, behavior)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+widgetColumns,
		id, workspaceID, name, publicKey, inboxID, modes, appearanceJSON, contentJSON, behaviorJSON,
	)
	return scanWidget(row)
}

func (r *repository) update(
	ctx context.Context, tx pgx.Tx, workspaceID, id, name string, inboxID *string, modes []string,
	appearance, content, behavior map[string]any, environment string, rolloutPercent int, enabled bool, version int,
) (*Widget, error) {
	appearanceJSON, err := json.Marshal(appearance)
	if err != nil {
		return nil, err
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	behaviorJSON, err := json.Marshal(behavior)
	if err != nil {
		return nil, err
	}
	row := tx.QueryRow(ctx, `
		UPDATE widgets SET
			name = $3, inbox_id = $4, modes = $5, appearance = $6, content = $7, behavior = $8,
			environment = $9, rollout_percent = $10, enabled = $11, version = $12, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING `+widgetColumns,
		workspaceID, id, name, inboxID, modes, appearanceJSON, contentJSON, behaviorJSON,
		environment, rolloutPercent, enabled, version,
	)
	return scanWidget(row)
}

func (r *repository) delete(ctx context.Context, tx pgx.Tx, workspaceID, id string) error {
	tag, err := tx.Exec(ctx, `DELETE FROM widgets WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("widget: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// touchInstall records the first (and every subsequent) config request seen
// from an allowed origin — the installation health signal WidgetList shows.
func (r *repository) touchInstall(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE widgets SET
			installed_at = coalesce(installed_at, now()),
			last_seen_at = now()
		WHERE id = $1
	`, id)
	return err
}

/* ---------------------------------------------------------- config versions */

const versionColumns = `
	id, widget_id, version, modes, appearance, content, behavior, changed_by, note, created_at
`

func scanVersion(row interface{ Scan(dest ...any) error }) (*ConfigVersion, error) {
	var v ConfigVersion
	err := row.Scan(
		&v.ID, &v.WidgetID, &v.Version, &v.Modes, &v.Appearance, &v.Content, &v.Behavior,
		&v.ChangedBy, &v.Note, &v.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *repository) insertVersion(
	ctx context.Context, tx pgx.Tx, id, widgetID string, version int,
	modes []string, appearance, content, behavior map[string]any, changedBy string, note *string,
) error {
	appearanceJSON, err := json.Marshal(appearance)
	if err != nil {
		return err
	}
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return err
	}
	behaviorJSON, err := json.Marshal(behavior)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO widget_config_versions (id, widget_id, version, modes, appearance, content, behavior, changed_by, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, id, widgetID, version, modes, appearanceJSON, contentJSON, behaviorJSON, changedBy, note)
	return err
}

func (r *repository) versions(ctx context.Context, widgetID string) ([]ConfigVersion, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+versionColumns+`
		FROM widget_config_versions WHERE widget_id = $1 ORDER BY version DESC
	`, widgetID)
	if err != nil {
		return nil, fmt.Errorf("widget: versions: %w", err)
	}
	defer rows.Close()

	out := []ConfigVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *repository) versionByNumber(ctx context.Context, widgetID string, version int) (*ConfigVersion, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+versionColumns+`
		FROM widget_config_versions WHERE widget_id = $1 AND version = $2
	`, widgetID, version)
	return scanVersion(row)
}

/* --------------------------------------------------------------- domains */

func (r *repository) domains(ctx context.Context, widgetID string) ([]Domain, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, widget_id, domain, verified_at, created_at
		FROM widget_domains WHERE widget_id = $1 ORDER BY created_at
	`, widgetID)
	if err != nil {
		return nil, fmt.Errorf("widget: domains: %w", err)
	}
	defer rows.Close()

	out := []Domain{}
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.WidgetID, &d.Domain, &d.VerifiedAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *repository) addDomain(ctx context.Context, tx pgx.Tx, id, widgetID, domain string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO widget_domains (id, widget_id, domain, verified_at)
		VALUES ($1, $2, $3, now())
	`, id, widgetID, domain)
	return err
}

func (r *repository) removeDomain(ctx context.Context, tx pgx.Tx, widgetID, domainID string) error {
	tag, err := tx.Exec(ctx, `DELETE FROM widget_domains WHERE widget_id = $1 AND id = $2`, widgetID, domainID)
	if err != nil {
		return fmt.Errorf("widget: remove domain: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) clearDomains(ctx context.Context, tx pgx.Tx, widgetID string) error {
	_, err := tx.Exec(ctx, `DELETE FROM widget_domains WHERE widget_id = $1`, widgetID)
	return err
}

// anyMemberOnline answers the widget's "online" badge — a member currently
// present and accepting conversations. This is the same two columns
// workspace/bootstrap.go already surfaces to the dashboard's presence
// indicator, read here from the widget's side of the same reality.
func (r *repository) anyMemberOnline(ctx context.Context, workspaceID string) (bool, error) {
	var online bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM workspace_members
			WHERE workspace_id = $1 AND presence = 'online' AND accepting_conversations
		)
	`, workspaceID).Scan(&online)
	return online, err
}

func uniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
