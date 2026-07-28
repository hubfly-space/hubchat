package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrAttrNotFound         = errors.New("customer: attribute definition not found")
	ErrAttrDuplicateKey     = errors.New("customer: an attribute with this key already exists")
	ErrAttrInvalidEntity    = errors.New("customer: not a recognised attribute entity type")
	ErrAttrInvalidType      = errors.New("customer: not a recognised attribute type")
	ErrAttrInvalidKey       = errors.New("customer: key must be lowercase letters, numbers, and underscores")
	ErrAttrNotDeclared      = errors.New("customer: this key is not declared in the metadata schema")
	ErrAttrSourceNotAllowed = errors.New("customer: this source is not permitted to set this attribute")
	ErrAttrBlockedKey       = errors.New("customer: this key matches a blocked pattern")
	ErrAttrInvalidValue     = errors.New("customer: value does not match the attribute's type or validation rules")
	ErrTooManyAttributes    = errors.New("customer: too many attributes on this record")
)

// AttributeValidation mirrors the shared FieldValidation wire shape.
type AttributeValidation struct {
	Min       *float64 `json:"min,omitempty"`
	Max       *float64 `json:"max,omitempty"`
	MinLength *int     `json:"min_length,omitempty"`
	MaxLength *int     `json:"max_length,omitempty"`
	Pattern   *string  `json:"pattern,omitempty"`
}

// AttributeDefinition is §6.10's metadata allowlist — separate from
// internal/ticket's FieldDefinition even though the shapes rhyme, because
// this table carries what only matters for untrusted input (migration 0006's
// own comment): which pipeline sources may write a key, and how long its
// values live.
type AttributeDefinition struct {
	ID                 string
	WorkspaceID        string
	EntityType         string
	Key                string
	Label              string
	Type               string
	Description        *string
	Options            []string
	AllowedSources     []string
	RequiredCapability *string
	Sensitive          bool
	Searchable         bool
	Validation         *AttributeValidation
	RetentionDays      *int
	ArchivedAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

const attributeDefinitionColumns = `
	id, workspace_id, entity_type, key, label, type, description, options, allowed_sources,
	required_capability, sensitive, searchable, validation, retention_days, archived_at, created_at, updated_at
`

func scanAttributeDefinition(row interface{ Scan(dest ...any) error }) (*AttributeDefinition, error) {
	var d AttributeDefinition
	err := row.Scan(
		&d.ID, &d.WorkspaceID, &d.EntityType, &d.Key, &d.Label, &d.Type, &d.Description, &d.Options, &d.AllowedSources,
		&d.RequiredCapability, &d.Sensitive, &d.Searchable, &d.Validation, &d.RetentionDays, &d.ArchivedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAttrNotFound
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

func nonNilAttrValidation(v *AttributeValidation) *AttributeValidation {
	if v == nil {
		return &AttributeValidation{}
	}
	return v
}

func (r *repository) insertAttributeDefinition(ctx context.Context, d AttributeDefinition) (*AttributeDefinition, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO attribute_definitions
			(id, workspace_id, entity_type, key, label, type, description, options, allowed_sources,
			 required_capability, sensitive, searchable, validation, retention_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+attributeDefinitionColumns,
		d.ID, d.WorkspaceID, d.EntityType, d.Key, d.Label, d.Type, d.Description, d.Options, d.AllowedSources,
		d.RequiredCapability, d.Sensitive, d.Searchable, nonNilAttrValidation(d.Validation), d.RetentionDays,
	)
	def, err := scanAttributeDefinition(row)
	if uniqueViolation(err) {
		return nil, ErrAttrDuplicateKey
	}
	return def, err
}

func (r *repository) attributeDefinitionByID(ctx context.Context, workspaceID, id string) (*AttributeDefinition, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+attributeDefinitionColumns+`
		FROM attribute_definitions WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	return scanAttributeDefinition(row)
}

func (r *repository) attributeDefinitionByKey(ctx context.Context, workspaceID, entityType, key string) (*AttributeDefinition, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+attributeDefinitionColumns+`
		FROM attribute_definitions
		WHERE workspace_id = $1 AND entity_type = $2 AND key = $3 AND archived_at IS NULL
	`, workspaceID, entityType, key)
	return scanAttributeDefinition(row)
}

func (r *repository) listAttributeDefinitions(ctx context.Context, workspaceID, entityType string) ([]AttributeDefinition, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+attributeDefinitionColumns+`
		FROM attribute_definitions
		WHERE workspace_id = $1 AND entity_type = $2 AND archived_at IS NULL
		ORDER BY key
	`, workspaceID, entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AttributeDefinition{}
	for rows.Next() {
		d, err := scanAttributeDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (r *repository) updateAttributeDefinition(ctx context.Context, workspaceID, id string, d AttributeDefinition) (*AttributeDefinition, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE attribute_definitions
		SET label = $3, description = $4, options = $5, allowed_sources = $6, required_capability = $7,
		    sensitive = $8, searchable = $9, validation = $10, retention_days = $11, updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND archived_at IS NULL
		RETURNING `+attributeDefinitionColumns,
		workspaceID, id, d.Label, d.Description, d.Options, d.AllowedSources, d.RequiredCapability,
		d.Sensitive, d.Searchable, nonNilAttrValidation(d.Validation), d.RetentionDays,
	)
	return scanAttributeDefinition(row)
}

func (r *repository) archiveAttributeDefinition(ctx context.Context, workspaceID, id string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE attribute_definitions SET archived_at = now(), updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND archived_at IS NULL
	`, workspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAttrNotFound
	}
	return nil
}

func (r *repository) blockedPatterns(ctx context.Context, workspaceID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT pattern FROM attribute_blocklist WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var pattern string
		if err := rows.Scan(&pattern); err != nil {
			return nil, err
		}
		out = append(out, pattern)
	}
	return out, rows.Err()
}

// mergeCustomerAttributes upserts values into the customer's attributes
// jsonb column with a shallow merge (Postgres' `||` operator), and returns
// the resulting key count so the caller can enforce
// cfg.Limits.MaxAttributesPerCustomer.
func (r *repository) mergeCustomerAttributes(ctx context.Context, tx pgx.Tx, workspaceID, customerID string, merged map[string]any) (int, error) {
	encoded, err := json.Marshal(merged)
	if err != nil {
		return 0, err
	}
	var count int
	err = tx.QueryRow(ctx, `
		UPDATE customers
		SET attributes = attributes || $3::jsonb, last_seen_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING (SELECT count(*) FROM jsonb_object_keys(attributes))
	`, workspaceID, customerID, encoded).Scan(&count)
	return count, err
}

func (r *repository) mergeCompanyAttributes(ctx context.Context, tx pgx.Tx, workspaceID, companyID string, merged map[string]any) (int, error) {
	encoded, err := json.Marshal(merged)
	if err != nil {
		return 0, err
	}
	var count int
	err = tx.QueryRow(ctx, `
		UPDATE companies
		SET attributes = attributes || $3::jsonb, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING (SELECT count(*) FROM jsonb_object_keys(attributes))
	`, workspaceID, companyID, encoded).Scan(&count)
	return count, err
}

// ---------------------------------------------------------------- service

var validAttrEntityTypes = map[string]bool{"customer": true, "company": true, "session": true}

var validAttrTypes = map[string]bool{
	"string": true, "integer": true, "decimal": true, "boolean": true, "timestamp": true,
	"date": true, "enum": true, "string_list": true, "url": true, "json": true,
}

var attrKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// AttributeDefinitionInput is every caller-supplied attribute of a
// definition, shared between create and update.
type AttributeDefinitionInput struct {
	Label              string
	Description        *string
	Options            []string
	AllowedSources     []string
	RequiredCapability *string
	Sensitive          bool
	Searchable         bool
	Validation         *AttributeValidation
	RetentionDays      *int
}

func (s *Service) CreateAttributeDefinition(ctx context.Context, workspaceID, entityType, key, attrType string, in AttributeDefinitionInput) (*AttributeDefinition, error) {
	if !validAttrEntityTypes[entityType] {
		return nil, ErrAttrInvalidEntity
	}
	if !validAttrTypes[attrType] {
		return nil, ErrAttrInvalidType
	}
	if !attrKeyPattern.MatchString(key) {
		return nil, ErrAttrInvalidKey
	}
	return s.repo.insertAttributeDefinition(ctx, AttributeDefinition{
		ID: ids.New(ids.PrefixAttributeDef), WorkspaceID: workspaceID, EntityType: entityType, Key: key, Type: attrType,
		Label: in.Label, Description: in.Description, Options: orEmptyStrings(in.Options),
		AllowedSources: orEmptyStrings(in.AllowedSources), RequiredCapability: in.RequiredCapability,
		Sensitive: in.Sensitive, Searchable: in.Searchable, Validation: in.Validation, RetentionDays: in.RetentionDays,
	})
}

func (s *Service) UpdateAttributeDefinition(ctx context.Context, workspaceID, id string, in AttributeDefinitionInput) (*AttributeDefinition, error) {
	return s.repo.updateAttributeDefinition(ctx, workspaceID, id, AttributeDefinition{
		Label: in.Label, Description: in.Description, Options: orEmptyStrings(in.Options),
		AllowedSources: orEmptyStrings(in.AllowedSources), RequiredCapability: in.RequiredCapability,
		Sensitive: in.Sensitive, Searchable: in.Searchable, Validation: in.Validation, RetentionDays: in.RetentionDays,
	})
}

func (s *Service) ArchiveAttributeDefinition(ctx context.Context, workspaceID, id string) error {
	return s.repo.archiveAttributeDefinition(ctx, workspaceID, id)
}

func (s *Service) ListAttributeDefinitions(ctx context.Context, workspaceID, entityType string) ([]AttributeDefinition, error) {
	if !validAttrEntityTypes[entityType] {
		return nil, ErrAttrInvalidEntity
	}
	return s.repo.listAttributeDefinitions(ctx, workspaceID, entityType)
}

func orEmptyStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// SetCustomerAttributes validates values against the metadata schema and
// merges them into customerID's record — the one path (§6.10) every
// attribute write goes through, whether it originates from an agent editing
// the dashboard or (once Stage 5's widget SDK exists) a browser event.
// source names which pipeline is writing, checked against each key's
// allowed_sources allowlist; 'rest_api' is what the authenticated dashboard
// API itself counts as.
func (s *Service) SetCustomerAttributes(ctx context.Context, workspaceID, actorMemberID, customerID, source string, values map[string]any) (*Customer, error) {
	if err := s.validateAttributeWrite(ctx, workspaceID, "customer", source, values); err != nil {
		return nil, err
	}

	var newCount int
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		newCount, err = s.repo.mergeCustomerAttributes(ctx, tx, workspaceID, customerID, values)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if s.maxAttributesPerRecord > 0 && newCount > s.maxAttributesPerRecord {
			return ErrTooManyAttributes
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "customer.attributes_set", EntityType: "customer", EntityID: customerID,
			Metadata: map[string]any{"keys": attributeKeys(values), "source": source},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "customer.updated",
			EntityType: "customer", EntityID: customerID, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": customerID, "attributes_updated": attributeKeys(values)},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, customerID)
}

func (s *Service) SetCompanyAttributes(ctx context.Context, workspaceID, actorMemberID, companyID, source string, values map[string]any) (*Company, error) {
	if err := s.validateAttributeWrite(ctx, workspaceID, "company", source, values); err != nil {
		return nil, err
	}

	var newCount int
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		newCount, err = s.repo.mergeCompanyAttributes(ctx, tx, workspaceID, companyID, values)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCompanyNotFound
			}
			return err
		}
		if s.maxAttributesPerRecord > 0 && newCount > s.maxAttributesPerRecord {
			return ErrTooManyAttributes
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "company.attributes_set", EntityType: entityCompany, EntityID: companyID,
			Metadata: map[string]any{"keys": attributeKeys(values), "source": source},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.companyByID(ctx, workspaceID, companyID)
}

func (s *Service) validateAttributeWrite(ctx context.Context, workspaceID, entityType, source string, values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	blocked, err := s.repo.blockedPatterns(ctx, workspaceID)
	if err != nil {
		return err
	}
	for key, value := range values {
		if isBlockedKey(key, blocked) {
			return fmt.Errorf("%w: %q", ErrAttrBlockedKey, key)
		}
		def, err := s.repo.attributeDefinitionByKey(ctx, workspaceID, entityType, key)
		if err != nil {
			if errors.Is(err, ErrAttrNotFound) {
				return fmt.Errorf("%w: %q", ErrAttrNotDeclared, key)
			}
			return err
		}
		if !contains(def.AllowedSources, source) {
			return fmt.Errorf("%w: %q via %q", ErrAttrSourceNotAllowed, key, source)
		}
		if err := validateAttributeValue(*def, value); err != nil {
			return fmt.Errorf("%w: %q", err, key)
		}
	}
	return nil
}

func isBlockedKey(key string, patterns []string) bool {
	for _, pattern := range patterns {
		re := globToRegexp(pattern)
		if re.MatchString(key) {
			return true
		}
	}
	return false
}

// globToRegexp turns a `*`-wildcard pattern like "*password*" into an anchored
// regexp — the blocked-key patterns are authored as simple globs (see the
// metadata schema settings page), not full regular expressions.
func globToRegexp(pattern string) *regexp.Regexp {
	parts := strings.Split(pattern, "*")
	for i, part := range parts {
		parts[i] = regexp.QuoteMeta(part)
	}
	re, err := regexp.Compile("^" + strings.Join(parts, ".*") + "$")
	if err != nil {
		return regexp.MustCompile(`$^`) // matches nothing
	}
	return re
}

func attributeKeys(values map[string]any) []string {
	out := make([]string, 0, len(values))
	for k := range values {
		out = append(out, k)
	}
	return out
}

// validateAttributeValue checks value against def's type and validation
// rules — the same shape ticket.validateFieldValue uses for custom fields,
// duplicated rather than shared because the two modules' field-type systems
// are allowed to diverge (§10.6 does not promise attributes and custom
// fields stay identical forever, only that they rhyme today).
func validateAttributeValue(def AttributeDefinition, value any) error {
	if value == nil {
		return nil
	}

	switch def.Type {
	case "string", "url":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: expected a string", ErrAttrInvalidValue)
		}
		if def.Validation != nil {
			if def.Validation.MinLength != nil && len(s) < *def.Validation.MinLength {
				return fmt.Errorf("%w: shorter than the minimum length", ErrAttrInvalidValue)
			}
			if def.Validation.MaxLength != nil && len(s) > *def.Validation.MaxLength {
				return fmt.Errorf("%w: longer than the maximum length", ErrAttrInvalidValue)
			}
			if def.Validation.Pattern != nil {
				re, err := regexp.Compile(*def.Validation.Pattern)
				if err == nil && !re.MatchString(s) {
					return fmt.Errorf("%w: does not match the required pattern", ErrAttrInvalidValue)
				}
			}
		}

	case "integer", "decimal":
		n, ok := value.(float64)
		if !ok {
			return fmt.Errorf("%w: expected a number", ErrAttrInvalidValue)
		}
		if def.Type == "integer" && n != float64(int64(n)) {
			return fmt.Errorf("%w: expected a whole number", ErrAttrInvalidValue)
		}
		if def.Validation != nil {
			if def.Validation.Min != nil && n < *def.Validation.Min {
				return fmt.Errorf("%w: below the minimum", ErrAttrInvalidValue)
			}
			if def.Validation.Max != nil && n > *def.Validation.Max {
				return fmt.Errorf("%w: above the maximum", ErrAttrInvalidValue)
			}
		}

	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: expected true or false", ErrAttrInvalidValue)
		}

	case "timestamp":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: expected an RFC 3339 timestamp string", ErrAttrInvalidValue)
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("%w: not a valid timestamp", ErrAttrInvalidValue)
		}

	case "date":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: expected a date string", ErrAttrInvalidValue)
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("%w: not a valid date", ErrAttrInvalidValue)
		}

	case "enum":
		s, ok := value.(string)
		if !ok || !contains(def.Options, s) {
			return fmt.Errorf("%w: not one of the configured options", ErrAttrInvalidValue)
		}

	case "string_list":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%w: expected a list of strings", ErrAttrInvalidValue)
		}
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return fmt.Errorf("%w: expected a list of strings", ErrAttrInvalidValue)
			}
		}

	case "json":
		// Already decoded from jsonb by the caller — any shape is valid.
	}

	return nil
}

func contains(options []string, s string) bool {
	for _, o := range options {
		if o == s {
			return true
		}
	}
	return false
}
