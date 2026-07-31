package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound       = errors.New("automation: not found")
	ErrInvalidName    = errors.New("automation: name is required")
	ErrInvalidTrigger = errors.New("automation: invalid trigger")
	ErrInvalidAction  = errors.New("automation: invalid action")
	ErrDepthExceeded  = errors.New("automation: causation depth exceeded")
	ErrRateLimited    = errors.New("automation: rule rate limit reached")
)

var triggers = map[string]bool{"conversation.created": true, "message.received": true, "ticket.created": true, "ticket.updated": true, "customer.identified": true, "customer.updated": true, "event.received": true, "form.submitted": true, "feedback.submitted": true, "sla.approaching": true, "sla.breached": true, "conversation.idle": true, "business_hours.changed": true, "schedule": true}
var actions = map[string]bool{"assign_member": true, "assign_team": true, "add_tag": true, "remove_tag": true, "set_priority": true, "set_field": true, "set_state": true, "send_message": true, "send_email": true, "invoke_webhook": true, "move_inbox": true, "start_sla": true, "pause_sla": true, "close_after_inactivity": true, "create_task": true}

type Service struct {
	pool     *database.Pool
	maxDepth int
	seenMu   sync.Mutex
	seen     map[string]int64
}
type Action struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Params map[string]any `json:"params"`
}
type Rule struct {
	ID             string         `json:"id"`
	WorkspaceID    string         `json:"workspace_id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Trigger        string         `json:"trigger"`
	Conditions     map[string]any `json:"conditions"`
	Actions        []Action       `json:"actions"`
	Position       int            `json:"position"`
	Enabled        bool           `json:"enabled"`
	MaxRunsPerHour *int           `json:"max_runs_per_hour,omitempty"`
	Version        int            `json:"version"`
	LastRunAt      *time.Time     `json:"last_run_at,omitempty"`
	RunCount24h    int            `json:"run_count_24h"`
	ErrorCount24h  int            `json:"error_count_24h"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
type Input struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Trigger        string         `json:"trigger"`
	Conditions     map[string]any `json:"conditions"`
	Actions        []Action       `json:"actions"`
	Position       int            `json:"position"`
	Enabled        bool           `json:"enabled"`
	MaxRunsPerHour *int           `json:"max_runs_per_hour"`
}
type Execution struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	RuleID         string    `json:"rule_id"`
	RuleVersion    int       `json:"rule_version"`
	EventID        *string   `json:"event_id,omitempty"`
	SubjectType    string    `json:"subject_type,omitempty"`
	SubjectID      string    `json:"subject_id,omitempty"`
	Outcome        string    `json:"outcome"`
	Depth          int       `json:"depth"`
	CausationID    string    `json:"causation_id,omitempty"`
	ActionsApplied []Action  `json:"actions_applied"`
	Error          string    `json:"error,omitempty"`
	DurationMS     int       `json:"duration_ms"`
	DryRun         bool      `json:"dry_run"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func New(pool *database.Pool) *Service {
	return &Service{pool: pool, maxDepth: 8, seen: make(map[string]int64)}
}
func (s *Service) Create(ctx context.Context, workspaceID, memberID string, input Input) (*Rule, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidName
	}
	if !triggers[input.Trigger] {
		return nil, ErrInvalidTrigger
	}
	if err := validateActions(input.Actions); err != nil {
		return nil, err
	}
	conditions, _ := json.Marshal(input.Conditions)
	actionsJSON, _ := json.Marshal(input.Actions)
	id := ids.New(ids.PrefixAutomationRule)
	_, err := s.pool.Exec(ctx, `INSERT INTO automation_rules(id,workspace_id,name,description,trigger,conditions,actions,position,enabled,max_runs_per_hour,created_by) VALUES($1,$2,$3,NULLIF($4,''),$5,$6::jsonb,$7::jsonb,$8,$9,$10,NULLIF($11,''))`, id, workspaceID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), input.Trigger, conditions, actionsJSON, input.Position, input.Enabled, input.MaxRunsPerHour, memberID)
	if err != nil {
		return nil, fmt.Errorf("automation: create: %w", err)
	}
	return s.Get(ctx, workspaceID, id)
}
func (s *Service) List(ctx context.Context, workspaceID string) ([]Rule, error) {
	rows, err := s.pool.Query(ctx, ruleQuery+` WHERE r.workspace_id=$1 ORDER BY r.position,r.created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Rule, 0)
	for rows.Next() {
		item, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}
func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Rule, error) {
	item, err := scanRule(s.pool.QueryRow(ctx, ruleQuery+` WHERE r.workspace_id=$1 AND r.id=$2`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}
func (s *Service) Update(ctx context.Context, workspaceID, memberID, id string, input Input) (*Rule, error) {
	current, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidName
	}
	if !triggers[input.Trigger] {
		return nil, ErrInvalidTrigger
	}
	if err := validateActions(input.Actions); err != nil {
		return nil, err
	}
	conditions, _ := json.Marshal(input.Conditions)
	actionsJSON, _ := json.Marshal(input.Actions)
	version := current.Version + 1
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO automation_rule_versions(id,rule_id,version,name,trigger,conditions,actions,changed_by) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,NULLIF($8,''))`, ids.New(ids.PrefixAutomationVersion), id, version, input.Name, input.Trigger, conditions, actionsJSON, memberID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE automation_rules SET name=$3,description=NULLIF($4,''),trigger=$5,conditions=$6::jsonb,actions=$7::jsonb,position=$8,enabled=$9,max_runs_per_hour=$10,version=$11,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), input.Trigger, conditions, actionsJSON, input.Position, input.Enabled, input.MaxRunsPerHour, version)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, workspaceID, id)
}
func (s *Service) Execute(ctx context.Context, workspaceID, ruleID, eventID, subjectType, subjectID, causationID string, depth int, dryRun bool) (*Execution, error) {
	return s.execute(ctx, workspaceID, ruleID, eventID, subjectType, subjectID, causationID, depth, dryRun, nil)
}

func (s *Service) execute(ctx context.Context, workspaceID, ruleID, eventID, subjectType, subjectID, causationID string, depth int, dryRun bool, subject map[string]any) (*Execution, error) {
	started := time.Now()
	rule, err := s.Get(ctx, workspaceID, ruleID)
	if err != nil {
		return nil, err
	}
	if depth > s.maxDepth {
		return s.recordExecution(ctx, workspaceID, rule, eventID, subjectType, subjectID, causationID, depth, "depth_exceeded", nil, ErrDepthExceeded, dryRun, started)
	}
	if !dryRun && rule.MaxRunsPerHour != nil {
		var count int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM automation_executions WHERE workspace_id=$1 AND rule_id=$2 AND outcome='matched' AND dry_run=false AND occurred_at>now()-interval '1 hour'`, workspaceID, ruleID).Scan(&count); err != nil {
			return nil, err
		}
		if count >= *rule.MaxRunsPerHour {
			return s.recordExecution(ctx, workspaceID, rule, eventID, subjectType, subjectID, causationID, depth, "rate_limited", nil, ErrRateLimited, dryRun, started)
		}
	}
	matched := matchesData(rule.Conditions, subject)
	if !matched {
		return s.recordExecution(ctx, workspaceID, rule, eventID, subjectType, subjectID, causationID, depth, "skipped", nil, nil, dryRun, started)
	}
	return s.recordExecution(ctx, workspaceID, rule, eventID, subjectType, subjectID, causationID, depth, "matched", rule.Actions, nil, dryRun, started)
}

// RunEventConsumer evaluates enabled rules from committed events. It starts
// at the first signal it sees so a deployment does not replay the whole event
// history, then drains every sequence gap. Execution rows are the durable
// acknowledgement; a restart can safely evaluate an event again because rate
// limits and action adapters are expected to be idempotent.
func (s *Service) RunEventConsumer(ctx context.Context, signals <-chan events.Signal, source interface {
	Since(context.Context, string, int64, int) ([]events.Record, error)
}) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-signals:
			if !ok {
				return
			}
			s.seenMu.Lock()
			after, exists := s.seen[signal.WorkspaceID]
			if !exists {
				after = signal.Sequence - 1
			}
			s.seenMu.Unlock()
			for {
				records, err := source.Since(ctx, signal.WorkspaceID, after, 200)
				if err != nil || len(records) == 0 {
					break
				}
				failed := false
				for _, record := range records {
					if err := s.processEvent(ctx, record); err != nil {
						failed = true
						break
					}
					after = record.Sequence
				}
				if failed {
					break
				}
				s.seenMu.Lock()
				s.seen[signal.WorkspaceID] = after
				s.seenMu.Unlock()
				if len(records) < 200 {
					break
				}
			}
		}
	}
}

func (s *Service) processEvent(ctx context.Context, record events.Record) error {
	trigger := triggerForEvent(record.Type)
	if trigger == "" {
		return nil
	}
	var subject map[string]any
	if len(record.Data) > 0 {
		_ = json.Unmarshal(record.Data, &subject)
	}
	if subject == nil {
		subject = map[string]any{}
	}
	subject["event.type"] = string(record.Type)
	subject["event.entity_type"] = record.EntityType
	subject["event.entity_id"] = record.EntityID
	rules, err := s.List(ctx, record.WorkspaceID)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.Trigger != trigger {
			continue
		}
		if _, err := s.execute(ctx, record.WorkspaceID, rule.ID, record.ID, record.EntityType, record.EntityID, record.ID, 0, false, subject); err != nil {
			return err
		}
	}
	return nil
}

func triggerForEvent(eventType events.Type) string {
	switch eventType {
	case events.ConversationCreated:
		return "conversation.created"
	case events.MessageCreated:
		return "message.received"
	case events.TicketCreated:
		return "ticket.created"
	case events.TicketUpdated, events.TicketStateSet:
		return "ticket.updated"
	case events.CustomerIdentified:
		return "customer.identified"
	case events.CustomerUpdated:
		return "customer.updated"
	case events.EventReceived:
		return "event.received"
	case events.FormSubmitted:
		return "form.submitted"
	case events.FeedbackCreated:
		return "feedback.submitted"
	case events.SLAApproaching:
		return "sla.approaching"
	case events.SLABreached:
		return "sla.breached"
	default:
		return ""
	}
}
func (s *Service) ListExecutions(ctx context.Context, workspaceID, ruleID string, limit int) ([]Execution, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT e.id,e.workspace_id,e.rule_id,e.rule_version,e.event_id,e.subject_type,e.subject_id,e.outcome,e.depth,e.causation_id,e.actions_applied,e.error,e.duration_ms,e.dry_run,e.occurred_at FROM automation_executions e WHERE e.workspace_id=$1`
	args := []any{workspaceID}
	if ruleID != "" {
		query += ` AND e.rule_id=$2`
		args = append(args, ruleID)
	}
	query += ` ORDER BY e.occurred_at DESC,e.id DESC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Execution, 0)
	for rows.Next() {
		item, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

const ruleQuery = `SELECT r.id,r.workspace_id,r.name,coalesce(r.description,''),r.trigger,r.conditions,r.actions,r.position,r.enabled,r.max_runs_per_hour,r.version,r.last_run_at,(SELECT count(*) FROM automation_executions e WHERE e.rule_id=r.id AND e.outcome='matched' AND e.occurred_at>now()-interval '24 hours'),(SELECT count(*) FROM automation_executions e WHERE e.rule_id=r.id AND e.outcome='failed' AND e.occurred_at>now()-interval '24 hours'),r.created_at,r.updated_at FROM automation_rules r`

func (s *Service) recordExecution(ctx context.Context, workspaceID string, rule *Rule, eventID, subjectType, subjectID, causationID string, depth int, outcome string, applied []Action, executionErr error, dryRun bool, started time.Time) (*Execution, error) {
	id := ids.New(ids.PrefixAutomationExecution)
	actionsJSON, _ := json.Marshal(applied)
	var event any
	if eventID != "" {
		event = eventID
	}
	errText := ""
	if executionErr != nil {
		errText = executionErr.Error()
	}
	var result Execution
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO automation_executions(id,workspace_id,rule_id,rule_version,event_id,subject_type,subject_id,outcome,depth,causation_id,actions_applied,error,duration_ms,dry_run) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''),$11::jsonb,NULLIF($12,''),$13,$14)`, id, workspaceID, rule.ID, rule.Version, event, subjectType, subjectID, outcome, depth, causationID, actionsJSON, errText, time.Since(started).Milliseconds(), dryRun); err != nil {
			return err
		}
		if !dryRun {
			_, err := tx.Exec(ctx, `UPDATE automation_rules SET last_run_at=now(),updated_at=updated_at WHERE workspace_id=$1 AND id=$2`, workspaceID, rule.ID)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result = Execution{ID: id, WorkspaceID: workspaceID, RuleID: rule.ID, RuleVersion: rule.Version, Outcome: outcome, Depth: depth, CausationID: causationID, ActionsApplied: applied, Error: errText, DurationMS: int(time.Since(started).Milliseconds()), DryRun: dryRun, OccurredAt: time.Now()}
	if eventID != "" {
		result.EventID = &eventID
	}
	result.SubjectType, result.SubjectID = subjectType, subjectID
	return &result, nil
}
func scanRule(row interface{ Scan(...any) error }) (*Rule, error) {
	var item Rule
	var conditions, actions []byte
	err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.Trigger, &conditions, &actions, &item.Position, &item.Enabled, &item.MaxRunsPerHour, &item.Version, &item.LastRunAt, &item.RunCount24h, &item.ErrorCount24h, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(conditions, &item.Conditions)
	_ = json.Unmarshal(actions, &item.Actions)
	return &item, nil
}
func scanExecution(row interface{ Scan(...any) error }) (*Execution, error) {
	var item Execution
	var actions []byte
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.RuleID, &item.RuleVersion, &item.EventID, &item.SubjectType, &item.SubjectID, &item.Outcome, &item.Depth, &item.CausationID, &actions, &item.Error, &item.DurationMS, &item.DryRun, &item.OccurredAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(actions, &item.ActionsApplied)
	return &item, nil
}
func validateActions(values []Action) error {
	for index := range values {
		if values[index].ID == "" {
			values[index].ID = fmt.Sprintf("action_%d", index+1)
		}
		if !actions[values[index].Type] {
			return ErrInvalidAction
		}
	}
	return nil
}
func matches(conditions map[string]any) bool {
	return matchesData(conditions, nil)
}

func matchesData(conditions map[string]any, subject map[string]any) bool {
	if len(conditions) == 0 {
		return true
	}
	raw, ok := conditions["conditions"].([]any)
	if !ok {
		return false
	}
	matchMode, _ := conditions["match"].(string)
	if matchMode != "any" {
		matchMode = "all"
	}
	results := make([]bool, 0, len(raw))
	for _, value := range raw {
		condition, ok := value.(map[string]any)
		if !ok {
			results = append(results, false)
			continue
		}
		field, _ := condition["field"].(string)
		operator, _ := condition["operator"].(string)
		actual, exists := subject[field]
		results = append(results, compareCondition(actual, exists, operator, condition["value"]))
	}
	if len(results) == 0 {
		return true
	}
	if matchMode == "any" {
		for _, result := range results {
			if result {
				return true
			}
		}
		return false
	}
	for _, result := range results {
		if !result {
			return false
		}
	}
	return true
}

func compareCondition(actual any, exists bool, operator string, expected any) bool {
	if operator == "is_set" {
		return exists && actual != nil && actual != ""
	}
	if !exists {
		return operator == "is_not"
	}
	left := fmt.Sprint(actual)
	right := fmt.Sprint(expected)
	switch operator {
	case "is":
		return left == right
	case "is_not":
		return left != right
	case "contains":
		return strings.Contains(strings.ToLower(left), strings.ToLower(right))
	case "in":
		value := reflect.ValueOf(expected)
		if value.IsValid() && (value.Kind() == reflect.Array || value.Kind() == reflect.Slice) {
			for i := 0; i < value.Len(); i++ {
				if left == fmt.Sprint(value.Index(i).Interface()) {
					return true
				}
			}
		}
		return false
	default:
		return false
	}
}
