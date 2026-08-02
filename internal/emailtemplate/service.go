// Package emailtemplate stores safe, workspace-scoped customer email overrides.
// Authentication mail remains in internal/mailer and is never customizable here.
package emailtemplate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound = errors.New("emailtemplate: not found")
	ErrInvalid  = errors.New("emailtemplate: invalid template")
)

type Template struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Description string     `json:"description"`
	Subject     string     `json:"subject"`
	Body        string     `json:"body"`
	Enabled     bool       `json:"enabled"`
	Source      string     `json:"source"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

type Input struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type Definition struct {
	Key         string
	Label       string
	Description string
	Subject     string
	Body        string
}

var definitions = []Definition{
	{Key: "ticket_created", Label: "Ticket created", Description: "Sent when a tracked support request is created.", Subject: "Your Hubchat request {{.TicketNumber}} was received", Body: "Hi {{.CustomerName}},\n\nWe received your support request {{.TicketNumber}}: {{.TicketTitle}}.\n\nOur team will follow up as soon as possible.\n\nHubchat"},
	{Key: "agent_replied", Label: "Agent replied", Description: "Sent when a support agent replies to a ticket.", Subject: "New reply on ticket {{.TicketNumber}}", Body: "Hi {{.CustomerName}},\n\n{{.AuthorName}} replied to your ticket {{.TicketNumber}} ({{.TicketTitle}}):\n\n{{.MessageBody}}\n\nView your request: {{.TicketLink}}\n\nHubchat"},
	{Key: "ticket_resolved", Label: "Ticket resolved", Description: "Sent when a ticket is resolved or closed.", Subject: "Your ticket {{.TicketNumber}} was resolved", Body: "Hi {{.CustomerName}},\n\nYour ticket {{.TicketNumber}} ({{.TicketTitle}}) is now {{.TicketStatus}}.\n\nIf you still need help, reply to this message.\n\nHubchat"},
	{Key: "survey_request", Label: "Survey request", Description: "Sent when a customer is invited to complete a survey.", Subject: "{{.SurveyName}}", Body: "Hi {{.CustomerName}},\n\nWe recently marked ticket {{.TicketNumber}} ({{.TicketTitle}}) as {{.TicketStatus}}. Would you take a moment to tell us how the support experience went?\n\nShare your feedback: {{.SurveyLink}}\n\nThank you,\nHubchat"},
	{Key: "portal_magic_link", Label: "Portal magic link", Description: "Sent when a customer requests a portal sign-in link.", Subject: "Your Hubchat portal sign-in link", Body: "Hi {{.CustomerName}},\n\nUse this link to sign in to your Hubchat portal:\n\n{{.PortalLink}}\n\nThis link expires in {{.ExpiresIn}}.\n\nHubchat"},
	{Key: "transcript_delivery", Label: "Transcript delivery", Description: "Sent when a customer requests a conversation transcript.", Subject: "Your Hubchat conversation transcript", Body: "Hi {{.CustomerName}},\n\nYour conversation transcript is ready:\n\n{{.TranscriptLink}}\n\nHubchat"},
	{Key: "feedback_update", Label: "Feedback update", Description: "Sent when followed feedback changes status.", Subject: "Feedback update: {{.TicketTitle}}", Body: "Hi {{.CustomerName}},\n\nThe feedback item {{.TicketTitle}} is now {{.TicketStatus}}.\n\nHubchat"},
	{Key: "changelog_published", Label: "Changelog published", Description: "Sent when a subscribed changelog entry is published.", Subject: "A new Hubchat update is available", Body: "Hi {{.CustomerName}},\n\n{{.MessageBody}}\n\nRead the update: {{.TicketLink}}\n\nHubchat"},
}

func Definitions() []Definition { return append([]Definition(nil), definitions...) }

func definition(key string) (Definition, bool) {
	for _, item := range definitions {
		if item.Key == key {
			return item, true
		}
	}
	return Definition{}, false
}

func Validate(key string, input Input) error {
	_, ok := definition(key)
	if !ok || strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.Body) == "" || len(input.Subject) > 300 || len(input.Body) > 20000 {
		return ErrInvalid
	}
	if strings.ContainsAny(input.Subject, "\r\n") || strings.Contains(input.Body, "<script") {
		return ErrInvalid
	}
	if _, err := parse(key, input.Subject); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalid, err)
	}
	if _, err := parse(key, input.Body); err != nil {
		return fmt.Errorf("%w: body: %v", ErrInvalid, err)
	}
	return nil
}

var simpleField = regexp.MustCompile(`\{\{\s*\.([A-Za-z][A-Za-z0-9_]*)\s*\}\}`)
var allowedFields = map[string]bool{"CustomerName": true, "TicketNumber": true, "TicketTitle": true, "TicketStatus": true, "AuthorName": true, "MessageBody": true, "TicketLink": true, "SurveyName": true, "SurveyLink": true, "PortalLink": true, "ExpiresIn": true, "TranscriptLink": true}

func parse(key, source string) (*template.Template, error) {
	for _, match := range simpleField.FindAllStringSubmatch(source, -1) {
		if !allowedFields[match[1]] {
			return nil, fmt.Errorf("unknown variable %q", match[1])
		}
	}
	stripped := simpleField.ReplaceAllString(source, "")
	if strings.Contains(stripped, "{{") || strings.Contains(stripped, "}}") {
		return nil, errors.New("only simple variables such as {{.CustomerName}} are supported")
	}
	return template.New(key).Option("missingkey=error").Parse(source)
}

func RenderText(subject, body string, values map[string]string) (string, string, error) {
	render := func(source string) (string, error) {
		t, err := template.New("email").Option("missingkey=error").Parse(source)
		if err != nil {
			return "", err
		}
		var out bytes.Buffer
		if err := t.Execute(&out, values); err != nil {
			return "", err
		}
		return out.String(), nil
	}
	resultSubject, err := render(subject)
	if err != nil {
		return "", "", err
	}
	resultBody, err := render(body)
	if err != nil {
		return "", "", err
	}
	return resultSubject, resultBody, nil
}

type Service struct{ pool *database.Pool }

func New(pool *database.Pool) *Service { return &Service{pool: pool} }

func (s *Service) List(ctx context.Context, workspaceID string) ([]Template, error) {
	rows, err := s.pool.Query(ctx, `SELECT key,subject,body,enabled,updated_at FROM email_template_overrides WHERE workspace_id=$1`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("emailtemplate: list: %w", err)
	}
	defer rows.Close()
	overrides := map[string]Template{}
	for rows.Next() {
		var item Template
		if err := rows.Scan(&item.Key, &item.Subject, &item.Body, &item.Enabled, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Source = "workspace"
		overrides[item.Key] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]Template, 0, len(definitions))
	for _, def := range definitions {
		item := Template{Key: def.Key, Label: def.Label, Description: def.Description, Subject: def.Subject, Body: def.Body, Enabled: true, Source: "default"}
		if override, ok := overrides[def.Key]; ok {
			override.Label = def.Label
			override.Description = def.Description
			item = override
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) Save(ctx context.Context, workspaceID, memberID, key string, input Input) (Template, error) {
	if err := Validate(key, input); err != nil {
		return Template{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO email_template_overrides(workspace_id,key,subject,body,enabled,updated_by) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(workspace_id,key) DO UPDATE SET subject=EXCLUDED.subject,body=EXCLUDED.body,enabled=EXCLUDED.enabled,updated_by=EXCLUDED.updated_by,updated_at=now()`, workspaceID, key, input.Subject, input.Body, enabled, memberID)
	if err != nil {
		return Template{}, fmt.Errorf("emailtemplate: save: %w", err)
	}
	items, err := s.List(ctx, workspaceID)
	if err != nil {
		return Template{}, err
	}
	for _, item := range items {
		if item.Key == key {
			return item, nil
		}
	}
	return Template{}, ErrNotFound
}

func (s *Service) Reset(ctx context.Context, workspaceID, key string) error {
	if _, ok := definition(key); !ok {
		return ErrNotFound
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM email_template_overrides WHERE workspace_id=$1 AND key=$2`, workspaceID, key)
	if err != nil {
		return fmt.Errorf("emailtemplate: reset: %w", err)
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	return nil
}

func (s *Service) Render(ctx context.Context, workspaceID, key string, values map[string]string) (string, string, error) {
	def, ok := definition(key)
	if !ok {
		return "", "", ErrNotFound
	}
	subject, body := def.Subject, def.Body
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT subject,body,enabled FROM email_template_overrides WHERE workspace_id=$1 AND key=$2`, workspaceID, key).Scan(&subject, &body, &enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		subject, body = def.Subject, def.Body
	} else if err != nil {
		return "", "", fmt.Errorf("emailtemplate: load: %w", err)
	} else if !enabled {
		subject, body = def.Subject, def.Body
	}
	return RenderText(subject, body, values)
}

// Preview renders user input without storing it. HTML is escaped in the sample
// values so previews cannot turn the dashboard into an HTML execution surface.
func Preview(input Input) (string, string, error) {
	values := map[string]string{}
	for _, field := range []string{"CustomerName", "TicketNumber", "TicketTitle", "TicketStatus", "AuthorName", "MessageBody", "TicketLink", "SurveyName", "SurveyLink", "PortalLink", "ExpiresIn", "TranscriptLink"} {
		values[field] = "Example " + field
	}
	return RenderText(input.Subject, input.Body, values)
}
