package ticket

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrInvalidEntityType = errors.New("ticket: not a recognised field entity type")
	ErrInvalidFieldType  = errors.New("ticket: not a recognised field type")
	ErrInvalidVisibility = errors.New("ticket: visibility must be \"public\" or \"internal\"")
	ErrInvalidKey        = errors.New("ticket: key must be lowercase letters, numbers, and underscores")
	ErrFieldRequired     = errors.New("ticket: this field is required")
	ErrInvalidFieldValue = errors.New("ticket: value does not match the field's type or validation rules")
)

// validEntityTypes mirrors field_definitions' entity_type CHECK constraint
// (migration 0005). Ticket is the only caller today, but the constraint
// already spans all four — validating against its full domain here costs
// nothing and means conversation/customer/company can start using this
// module the day they need custom fields, without a schema or validation
// change.
var validEntityTypes = map[string]bool{
	"ticket": true, "conversation": true, "customer": true, "company": true,
}

var validFieldTypes = map[string]bool{
	"string": true, "text": true, "integer": true, "decimal": true, "boolean": true,
	"timestamp": true, "date": true, "enum": true, "multi_enum": true, "string_list": true,
	"url": true, "email": true, "phone": true, "json": true,
}

var fieldKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// FieldDefinitionInput is every caller-supplied attribute of a field
// definition, shared between create and update so both validate the same
// way.
type FieldDefinitionInput struct {
	Label              string
	Description        *string
	Options            []string
	Required           bool
	Visibility         string
	Sensitive          bool
	Searchable         bool
	AllowedSources     []string
	RequiredCapability *string
	Validation         *FieldValidation
}

// CreateFieldDefinition adds a custom field to entityType's schema. Key is
// immutable from here on — TicketFields.tsx warns the caller before this,
// because a key is referenced by automation conditions, form definitions,
// and the API (§6.10), and silently changing it out from under those would
// break them without any error at the point of change.
func (s *Service) CreateFieldDefinition(
	ctx context.Context, workspaceID, entityType, key, fieldType string, in FieldDefinitionInput,
) (*FieldDefinition, error) {
	if !validEntityTypes[entityType] {
		return nil, ErrInvalidEntityType
	}
	if !validFieldTypes[fieldType] {
		return nil, ErrInvalidFieldType
	}
	if !fieldKeyPattern.MatchString(key) {
		return nil, ErrInvalidKey
	}
	if in.Visibility == "" {
		in.Visibility = "internal"
	}
	if in.Visibility != "public" && in.Visibility != "internal" {
		return nil, ErrInvalidVisibility
	}

	position, err := s.fields.nextPosition(ctx, workspaceID, entityType)
	if err != nil {
		return nil, err
	}

	return s.fields.insertDefinition(ctx, FieldDefinition{
		ID: ids.New(ids.PrefixFieldDefinition), WorkspaceID: workspaceID, EntityType: entityType,
		Key: key, Label: in.Label, Type: fieldType, Description: in.Description,
		Options: orEmptyStrings(in.Options), Required: in.Required, Visibility: in.Visibility,
		Sensitive: in.Sensitive, Searchable: in.Searchable, AllowedSources: orEmptyStrings(in.AllowedSources),
		RequiredCapability: in.RequiredCapability, Validation: in.Validation, Position: position,
	})
}

func (s *Service) UpdateFieldDefinition(ctx context.Context, workspaceID, id string, in FieldDefinitionInput) (*FieldDefinition, error) {
	if in.Visibility == "" {
		in.Visibility = "internal"
	}
	if in.Visibility != "public" && in.Visibility != "internal" {
		return nil, ErrInvalidVisibility
	}
	return s.fields.update(ctx, workspaceID, id, FieldDefinition{
		Label: in.Label, Description: in.Description, Options: orEmptyStrings(in.Options),
		Required: in.Required, Visibility: in.Visibility, Sensitive: in.Sensitive, Searchable: in.Searchable,
		AllowedSources: orEmptyStrings(in.AllowedSources), RequiredCapability: in.RequiredCapability, Validation: in.Validation,
	})
}

func (s *Service) ArchiveFieldDefinition(ctx context.Context, workspaceID, id string) error {
	return s.fields.archive(ctx, workspaceID, id)
}

func (s *Service) ListFieldDefinitions(ctx context.Context, workspaceID, entityType string) ([]FieldDefinition, error) {
	if !validEntityTypes[entityType] {
		return nil, ErrInvalidEntityType
	}
	return s.fields.list(ctx, workspaceID, entityType)
}

// ReorderFieldDefinitions applies a new display order in one pass — the
// drag-to-reorder handle TicketFields.tsx shows next to each row.
func (s *Service) ReorderFieldDefinitions(ctx context.Context, workspaceID string, orderedIDs []string) error {
	for i, id := range orderedIDs {
		if err := s.fields.setPosition(ctx, workspaceID, id, int16(i)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) FieldValues(ctx context.Context, workspaceID, entityType, entityID string) (map[string]any, error) {
	return s.fields.valuesForEntity(ctx, workspaceID, entityType, entityID)
}

func (s *Service) FieldValuesForMany(ctx context.Context, workspaceID, entityType string, entityIDs []string) (map[string]map[string]any, error) {
	return s.fields.valuesForMany(ctx, workspaceID, entityType, entityIDs)
}

// SetFieldValue validates value against key's definition and writes it in its
// own transaction — the single-field-value API path. setFieldValueTx is what
// ticket creation calls instead, so several values can be validated and
// written inside the same transaction as the ticket row itself.
func (s *Service) SetFieldValue(ctx context.Context, workspaceID, entityType, entityID, key string, value any) error {
	def, err := s.fields.byKey(ctx, workspaceID, entityType, key)
	if err != nil {
		return err
	}
	if err := validateFieldValue(*def, value); err != nil {
		return err
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.fields.setValue(ctx, tx, workspaceID, entityType, entityID, def.ID, value)
	})
}

// setFieldValueTx is SetFieldValue's in-transaction variant, for callers
// (ticket creation) that need several field writes to commit atomically with
// a row they are inserting in the same transaction.
func (s *Service) setFieldValueTx(ctx context.Context, tx pgx.Tx, workspaceID, entityType, entityID, key string, value any) error {
	def, err := s.fields.byKey(ctx, workspaceID, entityType, key)
	if err != nil {
		return err
	}
	if err := validateFieldValue(*def, value); err != nil {
		return err
	}
	return s.fields.setValue(ctx, tx, workspaceID, entityType, entityID, def.ID, value)
}

// validateFieldValue checks value against def's type and validation rules —
// §6.10's required/conditional field system enforced server-side, not just
// in the form that happens to be rendering it.
func validateFieldValue(def FieldDefinition, value any) error {
	if value == nil {
		if def.Required {
			return ErrFieldRequired
		}
		return nil
	}

	switch def.Type {
	case "string", "text", "url", "email", "phone":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: expected a string", ErrInvalidFieldValue)
		}
		if def.Validation != nil {
			if def.Validation.MinLength != nil && len(s) < *def.Validation.MinLength {
				return fmt.Errorf("%w: shorter than the minimum length", ErrInvalidFieldValue)
			}
			if def.Validation.MaxLength != nil && len(s) > *def.Validation.MaxLength {
				return fmt.Errorf("%w: longer than the maximum length", ErrInvalidFieldValue)
			}
			if def.Validation.Pattern != nil {
				re, err := regexp.Compile(*def.Validation.Pattern)
				if err == nil && !re.MatchString(s) {
					return fmt.Errorf("%w: does not match the required pattern", ErrInvalidFieldValue)
				}
			}
		}

	case "integer", "decimal":
		n, ok := value.(float64)
		if !ok {
			return fmt.Errorf("%w: expected a number", ErrInvalidFieldValue)
		}
		if def.Type == "integer" && n != float64(int64(n)) {
			return fmt.Errorf("%w: expected a whole number", ErrInvalidFieldValue)
		}
		if def.Validation != nil {
			if def.Validation.Min != nil && n < *def.Validation.Min {
				return fmt.Errorf("%w: below the minimum", ErrInvalidFieldValue)
			}
			if def.Validation.Max != nil && n > *def.Validation.Max {
				return fmt.Errorf("%w: above the maximum", ErrInvalidFieldValue)
			}
		}

	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: expected true or false", ErrInvalidFieldValue)
		}

	case "timestamp":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: expected an RFC 3339 timestamp string", ErrInvalidFieldValue)
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("%w: not a valid timestamp", ErrInvalidFieldValue)
		}

	case "date":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: expected a date string", ErrInvalidFieldValue)
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("%w: not a valid date", ErrInvalidFieldValue)
		}

	case "enum":
		s, ok := value.(string)
		if !ok || !contains(def.Options, s) {
			return fmt.Errorf("%w: not one of the configured options", ErrInvalidFieldValue)
		}

	case "multi_enum", "string_list":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%w: expected a list of strings", ErrInvalidFieldValue)
		}
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				return fmt.Errorf("%w: expected a list of strings", ErrInvalidFieldValue)
			}
			if def.Type == "multi_enum" && !contains(def.Options, s) {
				return fmt.Errorf("%w: not one of the configured options", ErrInvalidFieldValue)
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

func orEmptyStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
