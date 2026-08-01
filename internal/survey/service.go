// Package survey owns survey definitions, questions, responses, and deterministic aggregates.
package survey

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
)

var (
	ErrNotFound         = errors.New("survey: not found")
	ErrInvalidName      = errors.New("survey: name is required")
	ErrInvalidType      = errors.New("survey: invalid type")
	ErrInvalidQuestion  = errors.New("survey: invalid question")
	ErrClosed           = errors.New("survey: survey is closed")
	ErrAlreadyResponded = errors.New("survey: response already recorded")
)

var surveyTypes = map[string]bool{"csat": true, "ces": true, "nps": true, "custom": true}
var questionTypes = map[string]bool{"star": true, "stars": true, "number": true, "emoji": true, "choice": true, "multi_choice": true, "text": true, "boolean": true}

type Options struct {
	Jobs      *jobs.Client
	PublicURL *url.URL
	Events    *events.Log
}

type Service struct {
	pool      *database.Pool
	jobs      *jobs.Client
	publicURL *url.URL
	events    *events.Log
}

type Question struct {
	ID        string         `json:"id"`
	SurveyID  string         `json:"survey_id"`
	Prompt    string         `json:"prompt"`
	Type      string         `json:"type"`
	Options   []string       `json:"options,omitempty"`
	Required  bool           `json:"required"`
	Condition map[string]any `json:"condition,omitempty"`
	Position  int            `json:"position"`
}

type Survey struct {
	ID            string         `json:"id"`
	WorkspaceID   string         `json:"workspace_id"`
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Questions     []Question     `json:"questions"`
	Delivery      []string       `json:"delivery"`
	Trigger       map[string]any `json:"trigger"`
	Completion    map[string]any `json:"completion"`
	Anonymous     bool           `json:"anonymous"`
	MaxResponses  *int           `json:"max_responses,omitempty"`
	ResponseCount int            `json:"response_count"`
	SentCount     int            `json:"sent_count"`
	AverageScore  *float64       `json:"average_score,omitempty"`
	ResponseRate  *float64       `json:"response_rate,omitempty"`
	Enabled       bool           `json:"enabled"`
	ExpiresAt     *time.Time     `json:"expires_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type QuestionInput struct {
	Prompt    string         `json:"prompt"`
	Type      string         `json:"type"`
	Options   []string       `json:"options"`
	Required  bool           `json:"required"`
	Condition map[string]any `json:"condition"`
	Position  int            `json:"position"`
}

type Input struct {
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Delivery     []string        `json:"delivery"`
	Trigger      map[string]any  `json:"trigger"`
	Completion   map[string]any  `json:"completion"`
	Anonymous    bool            `json:"anonymous"`
	MaxResponses *int            `json:"max_responses"`
	ExpiresAt    *time.Time      `json:"expires_at"`
	Questions    []QuestionInput `json:"questions"`
}

type Response struct {
	ID             string         `json:"id"`
	SurveyID       string         `json:"survey_id"`
	CustomerID     *string        `json:"customer_id,omitempty"`
	ConversationID *string        `json:"conversation_id,omitempty"`
	TicketID       *string        `json:"ticket_id,omitempty"`
	AgentID        *string        `json:"agent_id,omitempty"`
	Score          *float64       `json:"score,omitempty"`
	Answers        map[string]any `json:"answers"`
	Comment        string         `json:"comment,omitempty"`
	SubmittedAt    *time.Time     `json:"submitted_at,omitempty"`
}

type ResponseInput struct {
	Token          string         `json:"token"`
	Score          *float64       `json:"score"`
	Answers        map[string]any `json:"answers"`
	Comment        string         `json:"comment"`
	ConversationID string         `json:"conversation_id"`
	TicketID       string         `json:"ticket_id"`
	AgentID        string         `json:"agent_id"`
}

type Summary struct {
	SurveyID      string           `json:"survey_id"`
	Type          string           `json:"type"`
	ResponseCount int64            `json:"response_count"`
	AverageScore  *float64         `json:"average_score,omitempty"`
	NPS           *float64         `json:"nps,omitempty"`
	CommentCount  int64            `json:"comment_count"`
	Distribution  map[string]int64 `json:"distribution"`
}

func New(pool *database.Pool, options ...Options) *Service {
	service := &Service{pool: pool}
	if len(options) > 0 {
		service.jobs = options[0].Jobs
		service.publicURL = options[0].PublicURL
		service.events = options[0].Events
	}
	return service
}

// RetentionSweep removes submitted responses past each workspace's configured
// survey window. Answers cascade with the response; aggregate reports remain
// deterministic over the retained response set.
func (s *Service) RetentionSweep(ctx context.Context) (int64, error) {
	var deleted int64
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH deleted AS (
				DELETE FROM survey_responses r
				USING workspaces w
				WHERE r.workspace_id = w.id
				  AND r.submitted_at IS NOT NULL
				  AND coalesce((w.settings #>> '{privacy,retention_days,survey_responses}')::int, 0) > 0
				  AND r.submitted_at < now() - make_interval(days => (w.settings #>> '{privacy,retention_days,survey_responses}')::int)
				  AND NOT EXISTS (
						SELECT 1 FROM workspace_legal_holds lh
						WHERE lh.workspace_id = w.id AND lh.released_at IS NULL
						  AND lh.category IN ('all', 'surveys')
					)
				RETURNING r.workspace_id, r.survey_id
			)
			SELECT workspace_id, survey_id, count(*) FROM deleted GROUP BY workspace_id, survey_id
		`)
		if err != nil {
			return fmt.Errorf("survey: retention delete: %w", err)
		}
		type retentionCount struct {
			workspaceID string
			surveyID    string
			count       int64
		}
		counts := make([]retentionCount, 0)
		for rows.Next() {
			var workspaceID, surveyID string
			var count int64
			if err := rows.Scan(&workspaceID, &surveyID, &count); err != nil {
				return fmt.Errorf("survey: retention counts: %w", err)
			}
			deleted += count
			counts = append(counts, retentionCount{workspaceID: workspaceID, surveyID: surveyID, count: count})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, item := range counts {
			if _, err := tx.Exec(ctx, `
				UPDATE surveys
				SET response_count = greatest(0, response_count - $3), updated_at = now()
				WHERE workspace_id = $1 AND id = $2
			`, item.workspaceID, item.surveyID, item.count); err != nil {
				return fmt.Errorf("survey: update retention count: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

func (s *Service) Create(ctx context.Context, workspaceID string, input Input) (*Survey, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrInvalidName
	}
	typ := strings.ToLower(strings.TrimSpace(input.Type))
	if typ == "" {
		typ = "csat"
	}
	if !surveyTypes[typ] {
		return nil, ErrInvalidType
	}
	questions, err := validateQuestions(input.Questions)
	if err != nil {
		return nil, err
	}
	delivery := input.Delivery
	if delivery == nil {
		delivery = []string{"email"}
	}
	trigger := surveyJSONObject(input.Trigger)
	completion := surveyJSONObject(input.Completion)
	id := ids.New(ids.PrefixSurvey)
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO surveys(id,workspace_id,name,type,delivery,trigger,completion,anonymous,max_responses,expires_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10)`, id, workspaceID, name, typ, delivery, trigger, completion, input.Anonymous, input.MaxResponses, input.ExpiresAt); err != nil {
			return err
		}
		for index, question := range questions {
			options := surveyJSONArray(question.Options)
			condition, _ := json.Marshal(question.Condition)
			if _, err := tx.Exec(ctx, `INSERT INTO survey_questions(id,survey_id,prompt,type,options,required,condition,position) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7::jsonb,$8)`, ids.New(ids.PrefixSurveyQuestion), id, question.Prompt, question.Type, options, question.Required, condition, index); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("survey: create: %w", err)
	}
	return s.Get(ctx, workspaceID, id)
}

// PostgreSQL JSONB defaults only apply when a column is omitted. Survey
// creation supplies these columns explicitly, so encoding a nil Go map/slice
// as JSON null would store a JSON null in columns whose contract is an object
// or array and make archive import fail later. Keep the wire shape stable at
// the service boundary instead.
func surveyJSONObject(value map[string]any) []byte {
	if value == nil {
		return []byte("{}")
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) == "null" {
		return []byte("{}")
	}
	return encoded
}

func surveyJSONArray(value []string) []byte {
	if value == nil {
		return []byte("[]")
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) == "null" {
		return []byte("[]")
	}
	return encoded
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Survey, error) {
	return s.ListPage(ctx, workspaceID, time.Time{}, "", 0)
}

// ListPage returns surveys in stable reverse-created order. The id tiebreaker
// keeps a page boundary deterministic when surveys share a creation timestamp.
func (s *Service) ListPage(ctx context.Context, workspaceID string, before time.Time, beforeID string, limit int) ([]Survey, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 201 {
		limit = 201
	}
	query := `SELECT s.id,s.workspace_id,s.name,s.type,s.delivery,s.trigger,s.completion,s.anonymous,s.max_responses,s.response_count,s.sent_count,AVG(r.score) FILTER (WHERE r.submitted_at IS NOT NULL),CASE WHEN s.sent_count=0 THEN NULL ELSE s.response_count::double precision/s.sent_count END,s.enabled,s.expires_at,s.created_at,s.updated_at FROM surveys s LEFT JOIN survey_responses r ON r.survey_id=s.id AND r.workspace_id=s.workspace_id WHERE s.workspace_id=$1`
	args := []any{workspaceID}
	if !before.IsZero() {
		query += ` AND (s.created_at,s.id) < ($2,$3)`
		args = append(args, before, beforeID)
	}
	query += ` GROUP BY s.id ORDER BY s.created_at DESC,s.id DESC`
	query += fmt.Sprintf(` LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Survey, 0)
	for rows.Next() {
		item, err := scanSurvey(rows)
		if err != nil {
			return nil, err
		}
		item.Questions, err = s.questions(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Survey, error) {
	row := s.pool.QueryRow(ctx, `SELECT s.id,s.workspace_id,s.name,s.type,s.delivery,s.trigger,s.completion,s.anonymous,s.max_responses,s.response_count,s.sent_count,AVG(r.score) FILTER (WHERE r.submitted_at IS NOT NULL),CASE WHEN s.sent_count=0 THEN NULL ELSE s.response_count::double precision/s.sent_count END,s.enabled,s.expires_at,s.created_at,s.updated_at FROM surveys s LEFT JOIN survey_responses r ON r.survey_id=s.id AND r.workspace_id=s.workspace_id WHERE s.workspace_id=$1 AND s.id=$2 GROUP BY s.id`, workspaceID, id)
	item, err := scanSurvey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Questions, err = s.questions(ctx, id)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) SetEnabled(ctx context.Context, workspaceID, id string, enabled bool) (*Survey, error) {
	result, err := s.pool.Exec(ctx, `UPDATE surveys SET enabled=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, enabled)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, workspaceID, id)
}

// NotifyTicketResolution creates one durable, one-time invitation for every
// enabled email survey that matches the ticket lifecycle event. The pending
// response and email job are committed together, so a worker retry cannot
// send a link for a response that was not recorded.
func (s *Service) NotifyTicketResolution(ctx context.Context, workspaceID, ticketID, sourceEventID, status string) error {
	if s.jobs == nil || strings.TrimSpace(sourceEventID) == "" {
		return nil
	}
	var customerID, email, name, number, title, agentID string
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, NULLIF(c.email::text,''), coalesce(c.name,''),
		       t.prefix || '-' || t.number::text, t.title, coalesce(t.assignee_id,'')
		FROM tickets t
		JOIN customers c ON c.id=t.customer_id AND c.workspace_id=t.workspace_id
		LEFT JOIN customer_notification_preferences preferences
		  ON preferences.customer_id=c.id AND preferences.workspace_id=c.workspace_id
		WHERE t.workspace_id=$1 AND t.id=$2
		  AND NULLIF(c.email::text,'') IS NOT NULL
		  AND coalesce(preferences.surveys,true)
		  AND NOT EXISTS (
			SELECT 1 FROM email_suppressions suppression
			WHERE suppression.workspace_id=c.workspace_id
			  AND suppression.address::text=c.email::text
		  )
	`, workspaceID, ticketID).Scan(&customerID, &email, &name, &number, &title, &agentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("survey: resolve ticket customer: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id,name,delivery,trigger,anonymous
		FROM surveys
		WHERE workspace_id=$1 AND enabled
		  AND (expires_at IS NULL OR expires_at > now())
		  AND delivery @> ARRAY['email']::text[]
		ORDER BY created_at,id
	`, workspaceID)
	if err != nil {
		return fmt.Errorf("survey: list delivery rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item struct {
			ID        string
			Name      string
			Delivery  []string
			Trigger   []byte
			Anonymous bool
		}
		if err := rows.Scan(&item.ID, &item.Name, &item.Delivery, &item.Trigger, &item.Anonymous); err != nil {
			return err
		}
		var trigger map[string]any
		if err := json.Unmarshal(item.Trigger, &trigger); err != nil {
			return fmt.Errorf("survey: decode trigger: %w", err)
		}
		if !triggerMatchesResolution(trigger, status) {
			continue
		}
		if err := s.issueInvitation(ctx, workspaceID, ticketID, sourceEventID, status, item.ID, item.Name, item.Anonymous, customerID, email, name, number, title, agentID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func triggerMatchesResolution(trigger map[string]any, status string) bool {
	event, _ := trigger["event"].(string)
	if event == "" {
		event, _ = trigger["on"].(string)
	}
	if event == "" {
		event = "ticket.resolved"
	}
	switch event {
	case "ticket.resolved":
		return status == "resolved"
	case "ticket.closed":
		return status == "closed"
	case "ticket.status_changed", "ticket.state_changed":
		return status == "resolved" || status == "closed"
	default:
		return false
	}
}

type surveyEmailPayload struct {
	To          string `json:"to"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	WorkspaceID string `json:"workspace_id"`
}

func (s *Service) issueInvitation(ctx context.Context, workspaceID, ticketID, sourceEventID, status, surveyID, surveyName string, anonymous bool, customerID, email, name, number, title, agentID string) error {
	token, err := auth.NewToken()
	if err != nil {
		return fmt.Errorf("survey: create invitation token: %w", err)
	}
	link := s.surveyLink(workspaceID, surveyID, token)
	subject := "How was your support experience?"
	if strings.TrimSpace(surveyName) != "" {
		subject = surveyName
	}
	body := fmt.Sprintf("Hi %s,\n\nWe recently marked ticket %s (%s) as %s. Would you take a moment to tell us how the support experience went?\n\nShare your feedback: %s\n\nThank you,\nHubchat", strings.TrimSpace(name), number, title, strings.ReplaceAll(status, "_", " "), link)
	storedCustomerID := customerID
	if anonymous {
		storedCustomerID = ""
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var responseID string
		err := tx.QueryRow(ctx, `
			INSERT INTO survey_responses
				(id,workspace_id,survey_id,customer_id,ticket_id,agent_id,token_hash,sent_at,source_event_id)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,now(),$8)
			ON CONFLICT (workspace_id,survey_id,source_event_id) DO NOTHING
			RETURNING id
		`, ids.New(ids.PrefixSurveyResponse), workspaceID, surveyID, storedCustomerID, ticketID, agentID, auth.HashToken(token), sourceEventID).Scan(&responseID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("survey: create invitation: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE surveys SET sent_count=sent_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, surveyID); err != nil {
			return fmt.Errorf("survey: count invitation: %w", err)
		}
		if _, err := jobs.EnqueueTx(ctx, tx, jobs.Spec{
			WorkspaceID: workspaceID,
			Queue:       "email",
			Type:        "email.send",
			Payload:     surveyEmailPayload{To: email, Subject: subject, Body: body, WorkspaceID: workspaceID},
			DedupeKey:   "survey-email:" + sourceEventID + ":" + surveyID + ":" + customerID,
		}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return fmt.Errorf("survey: queue invitation: %w", err)
		}
		return nil
	})
}

func (s *Service) surveyLink(workspaceID, surveyID, token string) string {
	path := "/portal/survey/" + url.PathEscape(workspaceID) + "/" + url.PathEscape(surveyID)
	if s.publicURL == nil {
		return path + "?token=" + url.QueryEscape(token)
	}
	base := *s.publicURL
	base.Path = strings.TrimRight(base.Path, "/") + path
	query := base.Query()
	query.Set("token", token)
	base.RawQuery = query.Encode()
	return base.String()
}

func (s *Service) Submit(ctx context.Context, workspaceID, id, customerID string, input ResponseInput) (*Response, error) {
	survey, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if !survey.Enabled || (survey.ExpiresAt != nil && time.Now().After(*survey.ExpiresAt)) || (survey.MaxResponses != nil && survey.ResponseCount >= *survey.MaxResponses) {
		return nil, ErrClosed
	}
	if err := validateAnswers(survey.Questions, input.Answers); err != nil {
		return nil, err
	}
	score := input.Score
	if score == nil {
		score = deriveScore(survey, input.Answers)
	}
	var response Response
	response.ID = ids.New(ids.PrefixSurveyResponse)
	storedCustomerID := customerID
	if survey.Anonymous {
		storedCustomerID = ""
	}
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var tokenHash []byte
		if strings.TrimSpace(input.Token) != "" {
			tokenHash = auth.HashToken(input.Token)
		}
		responseCustomerID := storedCustomerID
		pending := false
		if len(tokenHash) > 0 {
			var pendingCustomerID string
			err := tx.QueryRow(ctx, `
				SELECT id,coalesce(customer_id,'')
				FROM survey_responses
				WHERE workspace_id=$1 AND survey_id=$2 AND token_hash=$3 AND submitted_at IS NULL
				FOR UPDATE
			`, workspaceID, id, tokenHash).Scan(&response.ID, &pendingCustomerID)
			if err == nil {
				pending = true
				responseCustomerID = pendingCustomerID
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		storedCustomerID = responseCustomerID
		if !pending && responseCustomerID != "" {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM survey_responses WHERE workspace_id=$1 AND survey_id=$2 AND customer_id=$3 AND submitted_at IS NOT NULL)`, workspaceID, id, responseCustomerID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return ErrAlreadyResponded
			}
		}
		var customer any
		if responseCustomerID != "" {
			customer = responseCustomerID
		}
		var conversation any
		if input.ConversationID != "" {
			conversation = input.ConversationID
		}
		var ticket any
		if input.TicketID != "" {
			ticket = input.TicketID
		}
		var agent any
		if input.AgentID != "" {
			agent = input.AgentID
		}
		if pending {
			if _, err := tx.Exec(ctx, `UPDATE survey_responses SET score=$2,comment=NULLIF($3,''),submitted_at=now() WHERE workspace_id=$1 AND id=$4`, workspaceID, score, strings.TrimSpace(input.Comment), response.ID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `INSERT INTO survey_responses(id,workspace_id,survey_id,customer_id,conversation_id,ticket_id,agent_id,score,comment,token_hash,submitted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,now())`, response.ID, workspaceID, id, customer, conversation, ticket, agent, score, strings.TrimSpace(input.Comment), tokenHash); err != nil {
			return err
		}
		for _, question := range survey.Questions {
			value, present := input.Answers[question.ID]
			if !present {
				continue
			}
			encoded, _ := json.Marshal(value)
			if _, err := tx.Exec(ctx, `INSERT INTO survey_answers(response_id,question_id,value) VALUES($1,$2,$3::jsonb)`, response.ID, question.ID, encoded); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE surveys SET response_count=response_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
		if err != nil {
			return err
		}
		if s.events != nil {
			actorType := events.ActorSystem
			if storedCustomerID != "" {
				actorType = events.ActorCustomer
			}
			if _, err := s.events.Append(ctx, tx, events.Event{
				WorkspaceID: workspaceID,
				Type:        events.SurveyResponseCreated,
				EntityType:  "survey_response",
				EntityID:    response.ID,
				ActorType:   actorType,
				ActorID:     storedCustomerID,
				Data: map[string]any{
					"survey_id": id,
					"type":      survey.Type,
					"score":     score,
				},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	response.SurveyID, response.Score, response.Comment, response.Answers = id, score, strings.TrimSpace(input.Comment), input.Answers
	response.SubmittedAt = timePtr(time.Now())
	if storedCustomerID != "" {
		response.CustomerID = &storedCustomerID
	}
	return &response, nil
}

func (s *Service) ListResponses(ctx context.Context, workspaceID, surveyID string, limit int) ([]Response, error) {
	return s.ListResponsesPage(ctx, workspaceID, surveyID, time.Time{}, "", limit)
}

// ListResponsesPage returns submitted responses in a stable cursor order. A
// response cursor uses submitted_at plus id, so equal timestamps cannot cause
// rows to disappear or repeat while an operator pages through a large survey.
func (s *Service) ListResponsesPage(ctx context.Context, workspaceID, surveyID string, before time.Time, beforeID string, limit int) ([]Response, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT r.id,r.survey_id,r.customer_id,r.conversation_id,r.ticket_id,r.agent_id,r.score,r.comment,r.submitted_at,COALESCE(jsonb_object_agg(a.question_id,a.value) FILTER (WHERE a.question_id IS NOT NULL),'{}'::jsonb) FROM survey_responses r LEFT JOIN survey_answers a ON a.response_id=r.id WHERE r.workspace_id=$1 AND r.survey_id=$2 AND r.submitted_at IS NOT NULL`
	args := []any{workspaceID, surveyID}
	if !before.IsZero() {
		query += ` AND (r.submitted_at,r.id) < ($3,$4)`
		args = append(args, before, beforeID)
	}
	query += fmt.Sprintf(` GROUP BY r.id ORDER BY r.submitted_at DESC,r.id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Response, 0)
	for rows.Next() {
		var item Response
		var comment *string
		var raw []byte
		if err := rows.Scan(&item.ID, &item.SurveyID, &item.CustomerID, &item.ConversationID, &item.TicketID, &item.AgentID, &item.Score, &comment, &item.SubmittedAt, &raw); err != nil {
			return nil, err
		}
		if comment != nil {
			item.Comment = *comment
		}
		_ = json.Unmarshal(raw, &item.Answers)
		result = append(result, item)
	}
	return result, rows.Err()
}

// WriteResponsesCSV streams all submitted responses for an operator export.
// Answers remain one JSON column so custom question shapes stay lossless.
func (s *Service) WriteResponsesCSV(ctx context.Context, workspaceID, surveyID string, output io.Writer) error {
	if _, err := s.Get(ctx, workspaceID, surveyID); err != nil {
		return err
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id,r.customer_id,r.score,r.comment,r.submitted_at,COALESCE(jsonb_object_agg(a.question_id,a.value) FILTER (WHERE a.question_id IS NOT NULL),'{}'::jsonb) FROM survey_responses r LEFT JOIN survey_answers a ON a.response_id=r.id WHERE r.workspace_id=$1 AND r.survey_id=$2 AND r.submitted_at IS NOT NULL GROUP BY r.id ORDER BY r.submitted_at DESC,r.id DESC`, workspaceID, surveyID)
	if err != nil {
		return err
	}
	defer rows.Close()
	writer := csv.NewWriter(output)
	if err := writer.Write([]string{"id", "customer_id", "score", "comment", "submitted_at", "answers"}); err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var comment *string
		var customerID *string
		var score *float64
		var submittedAt time.Time
		var answers []byte
		if err := rows.Scan(&id, &customerID, &score, &comment, &submittedAt, &answers); err != nil {
			return err
		}
		customer := ""
		if customerID != nil {
			customer = *customerID
		}
		commentValue := ""
		if comment != nil {
			commentValue = *comment
		}
		scoreValue := ""
		if score != nil {
			scoreValue = fmt.Sprintf("%g", *score)
		}
		if err := writer.Write([]string{id, customer, scoreValue, commentValue, submittedAt.UTC().Format(time.RFC3339Nano), string(answers)}); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

func (s *Service) Summary(ctx context.Context, workspaceID, surveyID string) (*Summary, error) {
	survey, err := s.Get(ctx, workspaceID, surveyID)
	if err != nil {
		return nil, err
	}
	result := &Summary{SurveyID: survey.ID, Type: survey.Type, Distribution: map[string]int64{}}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE submitted_at IS NOT NULL), avg(score) FILTER (WHERE submitted_at IS NOT NULL),
		       count(*) FILTER (WHERE submitted_at IS NOT NULL AND comment IS NOT NULL AND comment <> '')
		FROM survey_responses WHERE workspace_id=$1 AND survey_id=$2
	`, workspaceID, surveyID).Scan(&result.ResponseCount, &result.AverageScore, &result.CommentCount); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT score::text, count(*) FROM survey_responses WHERE workspace_id=$1 AND survey_id=$2 AND submitted_at IS NOT NULL AND score IS NOT NULL GROUP BY score ORDER BY score`, workspaceID, surveyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var promoters, detractors, scored int64
	for rows.Next() {
		var score string
		var count int64
		if err := rows.Scan(&score, &count); err != nil {
			return nil, err
		}
		result.Distribution[score] = count
		scored += count
		var numeric float64
		if _, scanErr := fmt.Sscan(score, &numeric); scanErr == nil {
			switch {
			case numeric >= 9:
				promoters += count
			case numeric <= 6:
				detractors += count
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if survey.Type == "nps" && scored > 0 {
		value := float64(promoters-detractors) * 100 / float64(scored)
		result.NPS = &value
	}
	return result, nil
}

func (s *Service) questions(ctx context.Context, surveyID string) ([]Question, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,survey_id,prompt,type,options,required,condition,position FROM survey_questions WHERE survey_id=$1 ORDER BY position,id`, surveyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Question, 0)
	for rows.Next() {
		var item Question
		var options, condition []byte
		if err := rows.Scan(&item.ID, &item.SurveyID, &item.Prompt, &item.Type, &options, &item.Required, &condition, &item.Position); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(options, &item.Options)
		_ = json.Unmarshal(condition, &item.Condition)
		result = append(result, item)
	}
	return result, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanSurvey(row rowScanner) (*Survey, error) {
	var item Survey
	var trigger, completion []byte
	err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Type, &item.Delivery, &trigger, &completion, &item.Anonymous, &item.MaxResponses, &item.ResponseCount, &item.SentCount, &item.AverageScore, &item.ResponseRate, &item.Enabled, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(trigger, &item.Trigger)
	_ = json.Unmarshal(completion, &item.Completion)
	return &item, nil
}

func validateQuestions(input []QuestionInput) ([]QuestionInput, error) {
	result := make([]QuestionInput, len(input))
	copy(result, input)
	for index := range result {
		result[index].Prompt = strings.TrimSpace(result[index].Prompt)
		result[index].Type = strings.ToLower(strings.TrimSpace(result[index].Type))
		if result[index].Type == "stars" {
			result[index].Type = "star"
		}
		if result[index].Prompt == "" || !questionTypes[result[index].Type] {
			return nil, ErrInvalidQuestion
		}
		result[index].Position = index
	}
	return result, nil
}

func validateAnswers(questions []Question, answers map[string]any) error {
	for _, question := range questions {
		value, present := answers[question.ID]
		if question.Required && (!present || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "") {
			return ErrInvalidQuestion
		}
		if present {
			if question.Type == "choice" && len(question.Options) > 0 && !contains(question.Options, fmt.Sprint(value)) {
				return ErrInvalidQuestion
			}
		}
	}
	return nil
}

func deriveScore(survey *Survey, answers map[string]any) *float64 {
	if len(survey.Questions) == 0 {
		return nil
	}
	value, ok := answers[survey.Questions[0].ID]
	if !ok {
		return nil
	}
	var score float64
	switch value := value.(type) {
	case float64:
		score = value
	case int:
		score = float64(value)
	default:
		return nil
	}
	return &score
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func timePtr(value time.Time) *time.Time { return &value }
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
