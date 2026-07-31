// Package survey owns survey definitions, questions, responses, and deterministic aggregates.
package survey

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
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

type Service struct{ pool *database.Pool }

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

func New(pool *database.Pool) *Service { return &Service{pool: pool} }

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
	trigger, _ := json.Marshal(input.Trigger)
	completion, _ := json.Marshal(input.Completion)
	id := ids.New(ids.PrefixSurvey)
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO surveys(id,workspace_id,name,type,delivery,trigger,completion,anonymous,max_responses,expires_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10)`, id, workspaceID, name, typ, input.Delivery, trigger, completion, input.Anonymous, input.MaxResponses, input.ExpiresAt); err != nil {
			return err
		}
		for index, question := range questions {
			options, _ := json.Marshal(question.Options)
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

func (s *Service) List(ctx context.Context, workspaceID string) ([]Survey, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.workspace_id,s.name,s.type,s.delivery,s.trigger,s.completion,s.anonymous,s.max_responses,s.response_count,s.sent_count,AVG(r.score) FILTER (WHERE r.submitted_at IS NOT NULL),CASE WHEN s.sent_count=0 THEN NULL ELSE s.response_count::double precision/s.sent_count END,s.enabled,s.expires_at,s.created_at,s.updated_at FROM surveys s LEFT JOIN survey_responses r ON r.survey_id=s.id AND r.workspace_id=s.workspace_id WHERE s.workspace_id=$1 GROUP BY s.id ORDER BY s.created_at DESC`, workspaceID)
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
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if customerID != "" {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM survey_responses WHERE workspace_id=$1 AND survey_id=$2 AND customer_id=$3 AND submitted_at IS NOT NULL)`, workspaceID, id, customerID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return ErrAlreadyResponded
			}
		}
		var tokenHash []byte
		if strings.TrimSpace(input.Token) != "" {
			sum := sha256.Sum256([]byte(input.Token))
			tokenHash = sum[:]
		}
		if len(tokenHash) > 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM survey_responses WHERE token_hash=$1)`, tokenHash).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return ErrAlreadyResponded
			}
		}
		var customer any
		if customerID != "" {
			customer = customerID
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
		if _, err := tx.Exec(ctx, `INSERT INTO survey_responses(id,workspace_id,survey_id,customer_id,conversation_id,ticket_id,agent_id,score,comment,token_hash,submitted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,now())`, response.ID, workspaceID, id, customer, conversation, ticket, agent, score, strings.TrimSpace(input.Comment), tokenHash); err != nil {
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
		return err
	})
	if err != nil {
		return nil, err
	}
	response.SurveyID, response.Score, response.Comment, response.Answers = id, score, strings.TrimSpace(input.Comment), input.Answers
	response.SubmittedAt = timePtr(time.Now())
	if customerID != "" {
		response.CustomerID = &customerID
	}
	return &response, nil
}

func (s *Service) ListResponses(ctx context.Context, workspaceID, surveyID string, limit int) ([]Response, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id,r.survey_id,r.customer_id,r.conversation_id,r.ticket_id,r.agent_id,r.score,r.comment,r.submitted_at,COALESCE(jsonb_object_agg(a.question_id,a.value) FILTER (WHERE a.question_id IS NOT NULL),'{}'::jsonb) FROM survey_responses r LEFT JOIN survey_answers a ON a.response_id=r.id WHERE r.workspace_id=$1 AND r.survey_id=$2 AND r.submitted_at IS NOT NULL GROUP BY r.id ORDER BY r.submitted_at DESC,r.id DESC LIMIT $3`, workspaceID, surveyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Response, 0)
	for rows.Next() {
		var item Response
		var raw []byte
		if err := rows.Scan(&item.ID, &item.SurveyID, &item.CustomerID, &item.ConversationID, &item.TicketID, &item.AgentID, &item.Score, &item.Comment, &item.SubmittedAt, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &item.Answers)
		result = append(result, item)
	}
	return result, rows.Err()
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
