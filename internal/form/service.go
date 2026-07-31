package form

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/feedback"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/survey"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound          = errors.New("form: not found")
	ErrInvalidName       = errors.New("form: name is required")
	ErrInvalidSlug       = errors.New("form: slug must contain lowercase letters, numbers, and hyphens")
	ErrInvalidPurpose    = errors.New("form: purpose is not supported")
	ErrInvalidAccess     = errors.New("form: access is not supported")
	ErrInvalidField      = errors.New("form: field definition is invalid")
	ErrInvalidLimit      = errors.New("form: submission limit must be positive")
	ErrInvalidSubmission = errors.New("form: submission is invalid")
	ErrSubmissionLimit   = errors.New("form: submission limit reached")
	ErrRateLimited       = errors.New("form: submission rate limit reached")
	ErrDisabled          = errors.New("form: form is disabled")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type TargetServices struct {
	Conversation *conversation.Service
	Customer     *customer.Service
	Feedback     *feedback.Service
	Inbox        *inbox.Service
	Survey       *survey.Service
	Ticket       *ticket.Service
}

type Service struct {
	pool    *database.Pool
	targets TargetServices
}

type Field struct {
	ID           string
	Key          string
	Label        string
	Type         string
	Placeholder  *string
	Description  *string
	Options      []string
	Required     bool
	DefaultValue any
	Condition    map[string]any
	Validation   map[string]any
	Position     int
}

type Form struct {
	ID              string
	WorkspaceID     string
	Name            string
	Slug            string
	Description     *string
	Purpose         string
	Routing         map[string]any
	Confirmation    map[string]any
	Access          string
	SpamProtection  map[string]any
	MaxSubmissions  *int
	SubmissionCount int
	Enabled         bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Fields          []Field
}

type CreateInput struct {
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Description    *string        `json:"description"`
	Purpose        string         `json:"purpose"`
	Routing        map[string]any `json:"routing"`
	Confirmation   map[string]any `json:"confirmation"`
	Access         string         `json:"access"`
	SpamProtection map[string]any `json:"spam_protection"`
	MaxSubmissions *int           `json:"max_submissions"`
	Enabled        bool           `json:"enabled"`
	Fields         []FieldInput   `json:"fields"`
}

type UpdateInput = CreateInput

type FieldInput struct {
	Key          string         `json:"key"`
	Label        string         `json:"label"`
	Type         string         `json:"type"`
	Placeholder  *string        `json:"placeholder"`
	Description  *string        `json:"description"`
	Options      []string       `json:"options"`
	Required     bool           `json:"required"`
	DefaultValue any            `json:"default_value"`
	Condition    map[string]any `json:"condition"`
	Validation   map[string]any `json:"validation"`
	Position     int            `json:"position"`
}

type SubmissionInput struct {
	CustomerID string         `json:"customer_id"`
	VisitorID  string         `json:"visitor_id"`
	Values     map[string]any `json:"values"`
	SourceURL  string         `json:"source_url"`
	IP         string         `json:"ip"`
	UserAgent  string         `json:"user_agent"`
}

func New(pool *database.Pool, targets ...TargetServices) *Service {
	service := &Service{pool: pool}
	if len(targets) > 0 {
		service.targets = targets[0]
	}
	return service
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Form, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, slug, description, purpose, routing, confirmation,
		       access, spam_protection, max_submissions, submission_count, enabled, created_at, updated_at
		FROM forms WHERE workspace_id = $1 ORDER BY created_at DESC, id DESC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("form: list: %w", err)
	}
	defer rows.Close()
	forms := make([]Form, 0)
	for rows.Next() {
		item, err := scanForm(rows)
		if err != nil {
			return nil, err
		}
		forms = append(forms, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range forms {
		fields, err := s.fields(ctx, workspaceID, forms[i].ID)
		if err != nil {
			return nil, err
		}
		forms[i].Fields = fields
	}
	return forms, nil
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Form, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, slug, description, purpose, routing, confirmation,
		       access, spam_protection, max_submissions, submission_count, enabled, created_at, updated_at
		FROM forms WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	item, err := scanForm(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("form: get: %w", err)
	}
	item.Fields, err = s.fields(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) GetPublic(ctx context.Context, workspaceID, slug string) (*Form, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, name, slug, description, purpose, routing, confirmation,
		       access, spam_protection, max_submissions, submission_count, enabled, created_at, updated_at
		FROM forms WHERE workspace_id = $1 AND slug = $2 AND enabled
	`, workspaceID, strings.ToLower(strings.TrimSpace(slug)))
	item, err := scanForm(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Fields, err = s.fields(ctx, workspaceID, item.ID)
	return item, err
}

// ListPublic returns only enabled public forms for an embeddable surface. It
// intentionally does not expose authenticated forms or internal routing data.
func (s *Service) ListPublic(ctx context.Context, workspaceID string) ([]Form, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, name, slug, description, purpose, routing, confirmation,
		       access, spam_protection, max_submissions, submission_count, enabled, created_at, updated_at
		FROM forms WHERE workspace_id=$1 AND enabled AND access='public' ORDER BY name, id
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("form: list public: %w", err)
	}
	defer rows.Close()
	items := make([]Form, 0)
	for rows.Next() {
		item, scanErr := scanForm(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		item.Fields, scanErr = s.fields(ctx, workspaceID, item.ID)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Service) Create(ctx context.Context, workspaceID string, input CreateInput) (*Form, error) {
	if err := validateDefinition(input); err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixForm)
	routing, _ := json.Marshal(input.Routing)
	confirmation, _ := json.Marshal(input.Confirmation)
	spam, _ := json.Marshal(input.SpamProtection)
	if input.Routing == nil {
		routing = []byte(`{}`)
	}
	if input.Confirmation == nil {
		confirmation = []byte(`{}`)
	}
	if input.SpamProtection == nil {
		spam = []byte(`{}`)
	}
	if err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO forms (id, workspace_id, name, slug, description, purpose, routing,
			                    confirmation, access, spam_protection, max_submissions, enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`, id, workspaceID, strings.TrimSpace(input.Name), strings.ToLower(strings.TrimSpace(input.Slug)),
			input.Description, input.Purpose, routing, confirmation, input.Access, spam, input.MaxSubmissions, input.Enabled)
		if err != nil {
			return err
		}
		return replaceFields(ctx, tx, workspaceID, id, input.Fields)
	}); err != nil {
		return nil, fmt.Errorf("form: create: %w", err)
	}
	return s.Get(ctx, workspaceID, id)
}

func (s *Service) Update(ctx context.Context, workspaceID, id string, input UpdateInput) (*Form, error) {
	if err := validateDefinition(input); err != nil {
		return nil, err
	}
	routing, _ := json.Marshal(input.Routing)
	confirmation, _ := json.Marshal(input.Confirmation)
	spam, _ := json.Marshal(input.SpamProtection)
	if input.Routing == nil {
		routing = []byte(`{}`)
	}
	if input.Confirmation == nil {
		confirmation = []byte(`{}`)
	}
	if input.SpamProtection == nil {
		spam = []byte(`{}`)
	}
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			UPDATE forms SET name=$3, slug=$4, description=$5, purpose=$6, routing=$7,
				confirmation=$8, access=$9, spam_protection=$10, max_submissions=$11, enabled=$12, updated_at=now()
			WHERE workspace_id=$1 AND id=$2
		`, workspaceID, id, strings.TrimSpace(input.Name), strings.ToLower(strings.TrimSpace(input.Slug)), input.Description,
			input.Purpose, routing, confirmation, input.Access, spam, input.MaxSubmissions, input.Enabled)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return ErrNotFound
		}
		return replaceFields(ctx, tx, workspaceID, id, input.Fields)
	})
	if err != nil {
		return nil, fmt.Errorf("form: update: %w", err)
	}
	return s.Get(ctx, workspaceID, id)
}

func (s *Service) Delete(ctx context.Context, workspaceID, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM forms WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("form: delete: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Submit(ctx context.Context, workspaceID, slug string, input SubmissionInput) (string, error) {
	form, err := s.GetPublic(ctx, workspaceID, slug)
	if err != nil {
		return "", err
	}
	if !form.Enabled {
		return "", ErrDisabled
	}
	if form.Access == "authenticated" && input.CustomerID == "" {
		return "", fmt.Errorf("%w: sign-in is required for this form", ErrInvalidSubmission)
	}
	if err := validateSubmission(form.Fields, input.Values); err != nil {
		return "", err
	}
	if err := s.checkRateLimit(ctx, workspaceID, form, input.IP); err != nil {
		return "", err
	}

	id := ids.New(ids.PrefixSubmission)
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			UPDATE forms SET submission_count=submission_count+1, updated_at=now()
			WHERE workspace_id=$1 AND id=$2 AND (max_submissions IS NULL OR submission_count < max_submissions)
		`, workspaceID, form.ID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return ErrSubmissionLimit
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO form_submissions (id, workspace_id, form_id, customer_id, visitor_id, source_url, ip, user_agent)
			VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,'')::inet,NULLIF($8,''))
		`, id, workspaceID, form.ID, input.CustomerID, input.VisitorID, input.SourceURL, input.IP, input.UserAgent)
		if err != nil {
			return err
		}
		for _, field := range form.Fields {
			value, ok := input.Values[field.Key]
			if !ok {
				continue
			}
			encoded, encodeErr := json.Marshal(value)
			if encodeErr != nil {
				return fmt.Errorf("form: encode value: %w", encodeErr)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO form_submission_values (submission_id, field_id, value)
				SELECT $1, id, $3::jsonb FROM form_fields WHERE workspace_id=$2 AND id=$4
			`, id, workspaceID, encoded, field.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("form: submit: %w", err)
	}
	if err := s.routeSubmission(ctx, workspaceID, form, id, input); err != nil {
		// The accepted answers remain durable and visible to operators. A
		// failed target is held for repair rather than silently losing the
		// submission or creating an untracked target.
		_, _ = s.pool.Exec(ctx, `UPDATE form_submissions SET status='held' WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
		return "", fmt.Errorf("form: route submission: %w", err)
	}
	return id, nil
}

func (s *Service) checkRateLimit(ctx context.Context, workspaceID string, form *Form, ip string) error {
	if strings.TrimSpace(ip) == "" || form.SpamProtection == nil {
		return nil
	}
	limit := 0
	switch value := form.SpamProtection["rate_limit_per_hour"].(type) {
	case float64:
		limit = int(value)
	case int:
		limit = value
	}
	if limit <= 0 {
		return nil
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM form_submissions WHERE workspace_id=$1 AND form_id=$2 AND ip=$3::inet AND created_at>=now()-interval '1 hour'`, workspaceID, form.ID, ip).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) routeSubmission(ctx context.Context, workspaceID string, form *Form, submissionID string, input SubmissionInput) error {
	routing := form.Routing
	purpose := form.Purpose
	values := input.Values

	switch purpose {
	case "ticket":
		if s.targets.Ticket == nil {
			return errors.New("ticket target is unavailable")
		}
		inboxID, err := s.routingInbox(ctx, workspaceID, routing)
		if err != nil {
			return err
		}
		title := routedText(routing, values, "title_field", form.Name)
		body := routedBody(routing, values)
		req := ticket.CreateRequest{
			Title: title, Description: body, InboxID: inboxID, Channel: "form",
			Priority:   routedString(routing, "priority", "normal"),
			CustomerID: optionalInput(input.CustomerID),
			TeamID:     optionalRouting(routing, "team_id"),
			AssigneeID: optionalRouting(routing, "assignee_id"),
		}
		created, err := s.targets.Ticket.Create(ctx, workspaceID, "", req)
		if err != nil {
			return err
		}
		if err := s.applyRoutingTags(ctx, workspaceID, "ticket", created.ID, routing); err != nil {
			return err
		}
		return s.recordResult(ctx, workspaceID, submissionID, "ticket", created.ID)

	case "conversation":
		if s.targets.Conversation == nil {
			return errors.New("conversation target is unavailable")
		}
		inboxID, err := s.routingInbox(ctx, workspaceID, routing)
		if err != nil {
			return err
		}
		body := routedBody(routing, values)
		if strings.TrimSpace(body) == "" {
			body = form.Name
		}
		var customerID, visitorID *string
		if input.CustomerID != "" {
			customerID = &input.CustomerID
		}
		if input.VisitorID != "" {
			visitorID = &input.VisitorID
		}
		created, _, err := s.targets.Conversation.Start(ctx, workspaceID, inboxID, "form", nil, customerID, visitorID, "", body)
		if err != nil {
			return err
		}
		if err := s.applyRoutingTags(ctx, workspaceID, "conversation", created.ID, routing); err != nil {
			return err
		}
		return s.recordResult(ctx, workspaceID, submissionID, "conversation", created.ID)

	case "feedback":
		if s.targets.Feedback == nil {
			return errors.New("feedback target is unavailable")
		}
		boardID := strings.TrimSpace(routedString(routing, "board_id", ""))
		if boardID == "" {
			return errors.New("feedback routing requires board_id")
		}
		created, err := s.targets.Feedback.CreateItem(ctx, workspaceID, boardID, "", feedback.ItemInput{
			Title:       routedText(routing, values, "title_field", form.Name),
			Description: routedBody(routing, values),
			Type:        routedString(routing, "type", "feature_request"),
			Visibility:  routedString(routing, "visibility", "public"),
			Priority:    routedString(routing, "priority", ""),
		}, input.CustomerID)
		if err != nil {
			return err
		}
		return s.recordResult(ctx, workspaceID, submissionID, "feedback", created.ID)

	case "survey":
		if s.targets.Survey == nil {
			return errors.New("survey target is unavailable")
		}
		surveyID := strings.TrimSpace(routedString(routing, "survey_id", ""))
		if surveyID == "" {
			return errors.New("survey routing requires survey_id")
		}
		response, err := s.targets.Survey.Submit(ctx, workspaceID, surveyID, input.CustomerID, survey.ResponseInput{
			Token: routedString(routing, "token", ""), Answers: values,
			Comment: routedString(routing, "comment", ""),
		})
		if err != nil {
			return err
		}
		return s.recordResult(ctx, workspaceID, submissionID, "survey_response", response.ID)

	case "customer":
		if s.targets.Customer == nil {
			return errors.New("customer target is unavailable")
		}
		name := routedText(routing, values, "name_field", "")
		email := routedText(routing, values, "email_field", "")
		created, err := s.targets.Customer.Identify(ctx, workspaceID, optionalInput(input.CustomerID), optionalString(name), optionalString(email), nil, false)
		if err != nil {
			return err
		}
		return s.recordResult(ctx, workspaceID, submissionID, "customer", created.ID)
	default:
		return fmt.Errorf("unsupported form purpose %q", purpose)
	}
}

func (s *Service) routingInbox(ctx context.Context, workspaceID string, routing map[string]any) (string, error) {
	if id := optionalRouting(routing, "inbox_id"); id != nil {
		if s.targets.Inbox == nil {
			return "", errors.New("inbox target is unavailable")
		}
		if _, err := s.targets.Inbox.Get(ctx, workspaceID, *id); err != nil {
			return "", fmt.Errorf("form: invalid inbox routing: %w", err)
		}
		return *id, nil
	}
	if s.targets.Inbox == nil {
		return "", errors.New("form routing requires an inbox")
	}
	items, err := s.targets.Inbox.List(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.IsDefault {
			return item.ID, nil
		}
	}
	if len(items) == 0 {
		return "", errors.New("workspace has no inbox")
	}
	return items[0].ID, nil
}

func (s *Service) recordResult(ctx context.Context, workspaceID, submissionID, resultType, resultID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE form_submissions SET result_type=$3,result_id=$4 WHERE workspace_id=$1 AND id=$2`, workspaceID, submissionID, resultType, resultID)
	return err
}

func (s *Service) applyRoutingTags(ctx context.Context, workspaceID, subjectType, subjectID string, routing map[string]any) error {
	var raw []any
	switch values := routing["tag_ids"].(type) {
	case []any:
		raw = values
	case []string:
		for _, value := range values {
			raw = append(raw, value)
		}
	}
	for _, value := range raw {
		tagID, ok := value.(string)
		if !ok || strings.TrimSpace(tagID) == "" {
			continue
		}
		var err error
		switch subjectType {
		case "ticket":
			if s.targets.Ticket == nil {
				return errors.New("form: ticket target is unavailable")
			}
			err = s.targets.Ticket.AddTag(ctx, workspaceID, "", subjectID, tagID)
		case "conversation":
			if s.targets.Conversation == nil {
				return errors.New("form: conversation target is unavailable")
			}
			err = s.targets.Conversation.AddTag(ctx, workspaceID, "", subjectID, tagID)
		}
		if err != nil {
			return fmt.Errorf("form: apply routing tag: %w", err)
		}
	}
	return nil
}

func optionalInput(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalString(value string) *string { return optionalInput(value) }

func optionalRouting(routing map[string]any, key string) *string {
	return optionalInput(routedString(routing, key, ""))
}

func routedString(routing map[string]any, key, fallback string) string {
	if value, ok := routing[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func routedText(routing map[string]any, values map[string]any, fieldKey, fallback string) string {
	if key := routedString(routing, fieldKey, ""); key != "" {
		if value, ok := values[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return strings.TrimSpace(fmt.Sprint(value))
		}
	}
	for _, key := range []string{"title", "subject", "name", "email"} {
		if value, ok := values[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return strings.TrimSpace(fmt.Sprint(value))
		}
	}
	return fallback
}

func routedBody(routing map[string]any, values map[string]any) string {
	if key := routedString(routing, "body_field", ""); key != "" {
		return strings.TrimSpace(fmt.Sprint(values[key]))
	}
	parts := make([]string, 0, len(values))
	for key, value := range values {
		if strings.TrimSpace(fmt.Sprint(value)) == "" {
			continue
		}
		parts = append(parts, key+": "+strings.TrimSpace(fmt.Sprint(value)))
	}
	// Stable ordering keeps generated ticket/conversation bodies deterministic.
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func validateDefinition(input CreateInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return ErrInvalidName
	}
	if !slugPattern.MatchString(strings.ToLower(strings.TrimSpace(input.Slug))) {
		return ErrInvalidSlug
	}
	if input.Purpose != "ticket" && input.Purpose != "conversation" && input.Purpose != "feedback" && input.Purpose != "survey" && input.Purpose != "customer" {
		return ErrInvalidPurpose
	}
	if input.Access != "public" && input.Access != "authenticated" {
		return ErrInvalidAccess
	}
	if input.MaxSubmissions != nil && *input.MaxSubmissions <= 0 {
		return ErrInvalidLimit
	}
	seen := make(map[string]struct{}, len(input.Fields))
	for _, field := range input.Fields {
		key := strings.TrimSpace(field.Key)
		if key == "" || strings.TrimSpace(field.Label) == "" || !validFieldType(field.Type) {
			return ErrInvalidField
		}
		if _, exists := seen[key]; exists {
			return ErrInvalidField
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validFieldType(kind string) bool {
	switch kind {
	case "string", "text", "integer", "decimal", "boolean", "timestamp", "date", "enum", "multi_enum", "string_list", "url", "email", "phone", "json", "file", "rating", "hidden":
		return true
	default:
		return false
	}
}

func validateSubmission(fields []Field, values map[string]any) error {
	for _, field := range fields {
		value, present := values[field.Key]
		if !conditionApplies(field.Condition, values) {
			continue
		}
		if !present || value == nil || (field.Type == "string" || field.Type == "text" || field.Type == "email" || field.Type == "phone") && strings.TrimSpace(fmt.Sprint(value)) == "" {
			if field.Required {
				return fmt.Errorf("%w: %s is required", ErrInvalidSubmission, field.Key)
			}
			continue
		}
		switch field.Type {
		case "integer":
			n, ok := value.(float64)
			if !ok || n != float64(int64(n)) {
				return fmt.Errorf("%w: %s must be an integer", ErrInvalidSubmission, field.Key)
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%w: %s must be boolean", ErrInvalidSubmission, field.Key)
			}
		case "email":
			if !strings.Contains(fmt.Sprint(value), "@") {
				return fmt.Errorf("%w: %s must be an email", ErrInvalidSubmission, field.Key)
			}
		case "enum":
			if !contains(field.Options, fmt.Sprint(value)) {
				return fmt.Errorf("%w: %s is not an option", ErrInvalidSubmission, field.Key)
			}
		case "multi_enum":
			list, ok := value.([]any)
			if !ok {
				return fmt.Errorf("%w: %s must be a list", ErrInvalidSubmission, field.Key)
			}
			for _, option := range list {
				if !contains(field.Options, fmt.Sprint(option)) {
					return fmt.Errorf("%w: %s contains an invalid option", ErrInvalidSubmission, field.Key)
				}
			}
		case "rating":
			n, ok := value.(float64)
			if !ok || n < 1 || n > 5 {
				return fmt.Errorf("%w: %s must be between 1 and 5", ErrInvalidSubmission, field.Key)
			}
		}
	}
	return nil
}

func conditionApplies(condition map[string]any, values map[string]any) bool {
	if len(condition) == 0 {
		return true
	}
	field, _ := condition["field"].(string)
	operator, _ := condition["operator"].(string)
	if field == "" || operator == "" {
		return false
	}
	actual, exists := values[field]
	want := condition["value"]
	if !exists {
		return false
	}
	switch operator {
	case "equals", "is":
		return fmt.Sprint(actual) == fmt.Sprint(want)
	case "not_equals", "is_not":
		return fmt.Sprint(actual) != fmt.Sprint(want)
	case "contains":
		return strings.Contains(fmt.Sprint(actual), fmt.Sprint(want))
	default:
		return false
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func replaceFields(ctx context.Context, tx pgx.Tx, workspaceID, formID string, fields []FieldInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM form_fields WHERE workspace_id=$1 AND form_id=$2`, workspaceID, formID); err != nil {
		return err
	}
	for position, field := range fields {
		options, _ := json.Marshal(field.Options)
		validation, _ := json.Marshal(field.Validation)
		condition, _ := json.Marshal(field.Condition)
		defaultValue, _ := json.Marshal(field.DefaultValue)
		if field.Options == nil {
			options = []byte(`[]`)
		}
		if field.Validation == nil {
			validation = []byte(`{}`)
		}
		if field.Condition == nil {
			condition = nil
		}
		if field.DefaultValue == nil {
			defaultValue = nil
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO form_fields (id, workspace_id, form_id, key, label, type, placeholder,
				description, options, required, default_value, condition, validation, position)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		`, ids.New(ids.PrefixFormField), workspaceID, formID, strings.TrimSpace(field.Key), strings.TrimSpace(field.Label), field.Type,
			field.Placeholder, field.Description, options, field.Required, defaultValue, condition, validation, position); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) fields(ctx context.Context, workspaceID, formID string) ([]Field, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, key, label, type, placeholder, description, options, required,
		       default_value, condition, validation, position
		FROM form_fields WHERE workspace_id=$1 AND form_id=$2 ORDER BY position, id
	`, workspaceID, formID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Field, 0)
	for rows.Next() {
		var field Field
		var options, defaultValue, condition, validation []byte
		if err := rows.Scan(&field.ID, &field.Key, &field.Label, &field.Type, &field.Placeholder, &field.Description,
			&options, &field.Required, &defaultValue, &condition, &validation, &field.Position); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(options, &field.Options)
		if len(defaultValue) > 0 {
			_ = json.Unmarshal(defaultValue, &field.DefaultValue)
		}
		if len(condition) > 0 {
			_ = json.Unmarshal(condition, &field.Condition)
		}
		if len(validation) > 0 {
			_ = json.Unmarshal(validation, &field.Validation)
		}
		result = append(result, field)
	}
	return result, rows.Err()
}

func scanForm(row interface{ Scan(...any) error }) (*Form, error) {
	var item Form
	var routing, confirmation, spam []byte
	err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Slug, &item.Description, &item.Purpose,
		&routing, &confirmation, &item.Access, &spam, &item.MaxSubmissions, &item.SubmissionCount, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.Routing = map[string]any{}
	item.Confirmation = map[string]any{}
	item.SpamProtection = map[string]any{}
	_ = json.Unmarshal(routing, &item.Routing)
	_ = json.Unmarshal(confirmation, &item.Confirmation)
	_ = json.Unmarshal(spam, &item.SpamProtection)
	return &item, nil
}
