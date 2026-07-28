package ticket

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
)

var (
	ErrFieldNotFound = errors.New("ticket: field definition not found")
	ErrDuplicateKey  = errors.New("ticket: a field with this key already exists")
)

// FieldValidation mirrors the shared FieldValidation contract — min/max for
// numeric types, min_length/max_length/pattern for string-shaped ones.
type FieldValidation struct {
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	MinLength *int     `json:"min_length,omitempty"`
	MaxLength *int     `json:"max_length,omitempty"`
	Pattern   *string  `json:"pattern,omitempty"`
}

// FieldDefinition is §6.10's shared custom-field type system: one table
// backs every entity type that accepts custom fields (migration 0005's own
// comment explains why), owned here because tickets are the only entity that
// uses it so far — conversation/customer/company can call in the same way
// once they need it, rather than each growing their own copy.
type FieldDefinition struct {
	ID                 string
	WorkspaceID        string
	EntityType         string
	Key                string
	Label              string
	Type               string
	Description        *string
	Options            []string
	Required           bool
	Visibility         string
	Sensitive          bool
	Searchable         bool
	AllowedSources     []string
	RequiredCapability *string
	Validation         *FieldValidation
	Position           int16
	Condition          map[string]any
	ArchivedAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

const fieldDefinitionColumns = `
	id, workspace_id, entity_type, key, label, type, description, options, required,
	visibility, sensitive, searchable, allowed_sources, required_capability, validation,
	position, condition, archived_at, created_at, updated_at
`

func scanFieldDefinition(row interface{ Scan(dest ...any) error }) (*FieldDefinition, error) {
	var d FieldDefinition
	err := row.Scan(
		&d.ID, &d.WorkspaceID, &d.EntityType, &d.Key, &d.Label, &d.Type, &d.Description, &d.Options, &d.Required,
		&d.Visibility, &d.Sensitive, &d.Searchable, &d.AllowedSources, &d.RequiredCapability, &d.Validation,
		&d.Position, &d.Condition, &d.ArchivedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFieldNotFound
	}
	if err != nil {
		return nil, err
	}
	if d.Options == nil {
		d.Options = []string{}
	}
	if d.AllowedSources == nil {
		d.AllowedSources = []string{}
	}
	return &d, nil
}

type fieldRepository struct {
	pool *database.Pool
}

// nonNilValidation substitutes an empty struct for a nil pointer: the
// validation column is `jsonb NOT NULL DEFAULT '{}'`, and a Go nil pointer
// binds as a SQL NULL parameter (not the JSON literal `null`), which the
// NOT NULL constraint rejects outright.
func nonNilValidation(v *FieldValidation) *FieldValidation {
	if v == nil {
		return &FieldValidation{}
	}
	return v
}

func (r *fieldRepository) insertDefinition(ctx context.Context, d FieldDefinition) (*FieldDefinition, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO field_definitions
			(id, workspace_id, entity_type, key, label, type, description, options, required,
			 visibility, sensitive, searchable, allowed_sources, required_capability, validation, position)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING `+fieldDefinitionColumns,
		d.ID, d.WorkspaceID, d.EntityType, d.Key, d.Label, d.Type, d.Description, d.Options, d.Required,
		d.Visibility, d.Sensitive, d.Searchable, d.AllowedSources, d.RequiredCapability, nonNilValidation(d.Validation), d.Position,
	)
	def, err := scanFieldDefinition(row)
	if uniqueViolation(err) {
		return nil, ErrDuplicateKey
	}
	return def, err
}

func (r *fieldRepository) byID(ctx context.Context, workspaceID, id string) (*FieldDefinition, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+fieldDefinitionColumns+`
		FROM field_definitions WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	return scanFieldDefinition(row)
}

func (r *fieldRepository) byKey(ctx context.Context, workspaceID, entityType, key string) (*FieldDefinition, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+fieldDefinitionColumns+`
		FROM field_definitions
		WHERE workspace_id = $1 AND entity_type = $2 AND key = $3 AND archived_at IS NULL
	`, workspaceID, entityType, key)
	return scanFieldDefinition(row)
}

func (r *fieldRepository) list(ctx context.Context, workspaceID, entityType string) ([]FieldDefinition, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+fieldDefinitionColumns+`
		FROM field_definitions
		WHERE workspace_id = $1 AND entity_type = $2 AND archived_at IS NULL
		ORDER BY position, created_at
	`, workspaceID, entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []FieldDefinition{}
	for rows.Next() {
		d, err := scanFieldDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *fieldRepository) nextPosition(ctx context.Context, workspaceID, entityType string) (int16, error) {
	var max *int16
	err := r.pool.QueryRow(ctx, `
		SELECT max(position) FROM field_definitions WHERE workspace_id = $1 AND entity_type = $2
	`, workspaceID, entityType).Scan(&max)
	if err != nil {
		return 0, err
	}
	if max == nil {
		return 0, nil
	}
	return *max + 1, nil
}

func (r *fieldRepository) update(ctx context.Context, workspaceID, id string, d FieldDefinition) (*FieldDefinition, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE field_definitions
		SET label = $3, description = $4, options = $5, required = $6, visibility = $7,
		    sensitive = $8, searchable = $9, allowed_sources = $10, required_capability = $11,
		    validation = $12, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND archived_at IS NULL
		RETURNING `+fieldDefinitionColumns,
		workspaceID, id, d.Label, d.Description, d.Options, d.Required, d.Visibility,
		d.Sensitive, d.Searchable, d.AllowedSources, d.RequiredCapability, nonNilValidation(d.Validation),
	)
	return scanFieldDefinition(row)
}

func (r *fieldRepository) archive(ctx context.Context, workspaceID, id string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE field_definitions SET archived_at = now(), updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND archived_at IS NULL
	`, workspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFieldNotFound
	}
	return nil
}

func (r *fieldRepository) setPosition(ctx context.Context, workspaceID, id string, position int16) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE field_definitions SET position = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, position)
	return err
}

// -------------------------------------------------------------- values

// valuesForEntity returns entityID's custom field values keyed by the
// definition's key (not its id) — the shape the Ticket DTO's
// `field_values: Record<string, FieldValue>` expects. Only active
// (non-archived) definitions are read: an archived field's historical value
// still exists in the table but is not part of the entity's current record.
func (r *fieldRepository) valuesForEntity(ctx context.Context, workspaceID, entityType, entityID string) (map[string]any, error) {
	byEntity, err := r.valuesForMany(ctx, workspaceID, entityType, []string{entityID})
	if err != nil {
		return nil, err
	}
	return byEntity[entityID], nil
}

func (r *fieldRepository) valuesForMany(ctx context.Context, workspaceID, entityType string, entityIDs []string) (map[string]map[string]any, error) {
	out := make(map[string]map[string]any, len(entityIDs))
	if len(entityIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT fv.entity_id, fd.key, fv.value
		FROM field_values fv
		JOIN field_definitions fd ON fd.id = fv.definition_id
		WHERE fv.workspace_id = $1 AND fv.entity_type = $2 AND fv.entity_id = ANY($3)
		  AND fd.archived_at IS NULL
	`, workspaceID, entityType, entityIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var entityID, key string
		var value any
		if err := rows.Scan(&entityID, &key, &value); err != nil {
			return nil, err
		}
		if out[entityID] == nil {
			out[entityID] = map[string]any{}
		}
		out[entityID][key] = value
	}
	for _, id := range entityIDs {
		if out[id] == nil {
			out[id] = map[string]any{}
		}
	}
	return out, rows.Err()
}

func (r *fieldRepository) setValue(ctx context.Context, tx pgx.Tx, workspaceID, entityType, entityID, definitionID string, value any) error {
	// pgx only auto-marshals values that are not already a string or []byte —
	// a bare Go string like "acct_123" would otherwise be sent verbatim as
	// jsonb text, which Postgres rejects as invalid JSON (it needs quotes).
	// Marshaling explicitly here makes every value shape — string, number,
	// bool, slice, map, nil — arrive as well-formed JSON regardless.
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO field_values (workspace_id, definition_id, entity_type, entity_id, value)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (definition_id, entity_id) DO UPDATE SET value = $5, updated_at = now()
	`, workspaceID, definitionID, entityType, entityID, encoded)
	return err
}
