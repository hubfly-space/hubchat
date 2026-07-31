package sla

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound      = errors.New("sla: not found")
	ErrInvalidName   = errors.New("sla: name is required")
	ErrInvalidTarget = errors.New("sla: target must be non-negative")
)

var priorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}

type Service struct {
	pool   *database.Pool
	events *events.Log
	seenMu sync.Mutex
	seen   map[string]int64
}
type Holiday struct {
	ID   string `json:"id"`
	Date string `json:"date"`
	Name string `json:"name"`
}
type CalendarRecord struct {
	ID          string      `json:"id"`
	WorkspaceID string      `json:"workspace_id"`
	Name        string      `json:"name"`
	Timezone    string      `json:"timezone"`
	Weekly      [7][]Window `json:"weekly"`
	Holidays    []Holiday   `json:"holidays"`
	IsDefault   bool        `json:"is_default"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}
type CalendarInput struct {
	Name      string      `json:"name"`
	Timezone  string      `json:"timezone"`
	Weekly    [7][]Window `json:"weekly"`
	Holidays  []Holiday   `json:"holidays"`
	IsDefault bool        `json:"is_default"`
}
type Target struct {
	ID                   string `json:"id"`
	Priority             string `json:"priority"`
	FirstResponseMinutes *int   `json:"first_response_minutes"`
	NextResponseMinutes  *int   `json:"next_response_minutes"`
	ResolutionMinutes    *int   `json:"resolution_minutes"`
}
type Policy struct {
	ID                      string           `json:"id"`
	WorkspaceID             string           `json:"workspace_id"`
	Name                    string           `json:"name"`
	Description             string           `json:"description,omitempty"`
	CalendarID              *string          `json:"calendar_id,omitempty"`
	Targets                 []Target         `json:"targets"`
	PauseStates             []string         `json:"pause_states"`
	WarningThresholdPercent int              `json:"warning_threshold_percent"`
	EscalationActions       []map[string]any `json:"escalation_actions"`
	AppliesTo               map[string]any   `json:"applies_to"`
	Enabled                 bool             `json:"enabled"`
	CreatedAt               time.Time        `json:"created_at"`
	UpdatedAt               time.Time        `json:"updated_at"`
}
type PolicyInput struct {
	Name                    string           `json:"name"`
	Description             string           `json:"description"`
	CalendarID              string           `json:"calendar_id"`
	Targets                 []Target         `json:"targets"`
	PauseStates             []string         `json:"pause_states"`
	WarningThresholdPercent int              `json:"warning_threshold_percent"`
	EscalationActions       []map[string]any `json:"escalation_actions"`
	AppliesTo               map[string]any   `json:"applies_to"`
	Enabled                 *bool            `json:"enabled"`
}

func New(pool *database.Pool, eventLog ...*events.Log) *Service {
	var log *events.Log
	if len(eventLog) > 0 {
		log = eventLog[0]
	}
	return &Service{pool: pool, events: log, seen: make(map[string]int64)}
}

func (s *Service) CreateCalendar(ctx context.Context, workspaceID string, input CalendarInput) (*CalendarRecord, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidName
	}
	if input.Timezone == "" {
		input.Timezone = "UTC"
	}
	if _, err := NewCalendar(input.Timezone, input.Weekly, holidayDates(input.Holidays)); err != nil {
		return nil, err
	}
	weekly, _ := json.Marshal(input.Weekly)
	id := ids.New(ids.PrefixCalendar)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if input.IsDefault {
			if _, err := tx.Exec(ctx, `UPDATE business_hour_calendars SET is_default=false,updated_at=now() WHERE workspace_id=$1`, workspaceID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO business_hour_calendars(id,workspace_id,name,timezone,weekly,is_default) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, id, workspaceID, strings.TrimSpace(input.Name), input.Timezone, weekly, input.IsDefault); err != nil {
			return err
		}
		for _, holiday := range input.Holidays {
			if _, err := tx.Exec(ctx, `INSERT INTO calendar_holidays(id,calendar_id,name,date) VALUES($1,$2,$3,$4)`, ids.New(ids.PrefixHoliday), id, strings.TrimSpace(holiday.Name), holiday.Date); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sla: create calendar: %w", err)
	}
	return s.GetCalendar(ctx, workspaceID, id)
}

func (s *Service) ListCalendars(ctx context.Context, workspaceID string) ([]CalendarRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,timezone,weekly,is_default,created_at,updated_at FROM business_hour_calendars WHERE workspace_id=$1 ORDER BY is_default DESC,name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]CalendarRecord, 0)
	for rows.Next() {
		item, err := scanCalendar(rows)
		if err != nil {
			return nil, err
		}
		item.Holidays, err = s.holidays(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}
func (s *Service) GetCalendar(ctx context.Context, workspaceID, id string) (*CalendarRecord, error) {
	item, err := scanCalendar(s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,timezone,weekly,is_default,created_at,updated_at FROM business_hour_calendars WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Holidays, err = s.holidays(ctx, id)
	return item, err
}

func (s *Service) CreatePolicy(ctx context.Context, workspaceID string, input PolicyInput) (*Policy, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidName
	}
	if input.WarningThresholdPercent == 0 {
		input.WarningThresholdPercent = 80
	}
	if input.WarningThresholdPercent < 1 || input.WarningThresholdPercent > 100 {
		return nil, ErrInvalidTarget
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.PauseStates == nil {
		input.PauseStates = []string{"waiting_for_customer"}
	}
	for _, target := range input.Targets {
		if !priorities[target.Priority] {
			return nil, ErrInvalidTarget
		}
		if target.FirstResponseMinutes != nil && *target.FirstResponseMinutes < 0 || target.NextResponseMinutes != nil && *target.NextResponseMinutes < 0 || target.ResolutionMinutes != nil && *target.ResolutionMinutes < 0 {
			return nil, ErrInvalidTarget
		}
	}
	actions, _ := json.Marshal(input.EscalationActions)
	applies, _ := json.Marshal(input.AppliesTo)
	id := ids.New(ids.PrefixSLAPolicy)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var calendar any
		if input.CalendarID != "" {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_hour_calendars WHERE workspace_id=$1 AND id=$2)`, workspaceID, input.CalendarID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
			calendar = input.CalendarID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sla_policies(id,workspace_id,name,description,calendar_id,pause_states,warning_threshold_percent,escalation_actions,applies_to,enabled) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8::jsonb,$9::jsonb,$10)`, id, workspaceID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), calendar, input.PauseStates, input.WarningThresholdPercent, actions, applies, enabled); err != nil {
			return err
		}
		for _, target := range input.Targets {
			if _, err := tx.Exec(ctx, `INSERT INTO sla_policy_targets(id,policy_id,priority,first_response_minutes,next_response_minutes,resolution_minutes) VALUES($1,$2,$3,$4,$5,$6)`, ids.New(ids.PrefixSLATarget), id, target.Priority, target.FirstResponseMinutes, target.NextResponseMinutes, target.ResolutionMinutes); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sla: create policy: %w", err)
	}
	return s.GetPolicy(ctx, workspaceID, id)
}

func (s *Service) ListPolicies(ctx context.Context, workspaceID string) ([]Policy, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,coalesce(description,''),calendar_id,pause_states,warning_threshold_percent,escalation_actions,applies_to,enabled,created_at,updated_at FROM sla_policies WHERE workspace_id=$1 ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Policy, 0)
	for rows.Next() {
		item, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		item.Targets, err = s.targets(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}
func (s *Service) GetPolicy(ctx context.Context, workspaceID, id string) (*Policy, error) {
	item, err := scanPolicy(s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,coalesce(description,''),calendar_id,pause_states,warning_threshold_percent,escalation_actions,applies_to,enabled,created_at,updated_at FROM sla_policies WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Targets, err = s.targets(ctx, id)
	return item, err
}
func (s *Service) SetPolicyEnabled(ctx context.Context, workspaceID, id string, enabled bool) (*Policy, error) {
	result, err := s.pool.Exec(ctx, `UPDATE sla_policies SET enabled=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, enabled)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetPolicy(ctx, workspaceID, id)
}

// UpdatePolicy replaces the mutable policy configuration in one transaction.
// Targets are replaced together with the policy row so a failed validation or
// insert cannot leave a policy with only part of its priority matrix.
func (s *Service) UpdatePolicy(ctx context.Context, workspaceID, id string, input PolicyInput) (*Policy, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidName
	}
	if input.WarningThresholdPercent < 1 || input.WarningThresholdPercent > 100 {
		return nil, ErrInvalidTarget
	}
	if input.PauseStates == nil {
		input.PauseStates = []string{}
	}
	for _, target := range input.Targets {
		if !priorities[target.Priority] {
			return nil, ErrInvalidTarget
		}
		if (target.FirstResponseMinutes != nil && *target.FirstResponseMinutes < 0) ||
			(target.NextResponseMinutes != nil && *target.NextResponseMinutes < 0) ||
			(target.ResolutionMinutes != nil && *target.ResolutionMinutes < 0) {
			return nil, ErrInvalidTarget
		}
	}
	actions, err := json.Marshal(input.EscalationActions)
	if err != nil {
		return nil, fmt.Errorf("sla: encode escalation actions: %w", err)
	}
	applies, err := json.Marshal(input.AppliesTo)
	if err != nil {
		return nil, fmt.Errorf("sla: encode policy scope: %w", err)
	}
	var calendar any
	if strings.TrimSpace(input.CalendarID) != "" {
		calendar = strings.TrimSpace(input.CalendarID)
	}
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if calendar != nil {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_hour_calendars WHERE workspace_id=$1 AND id=$2)`, workspaceID, calendar).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
		}
		result, err := tx.Exec(ctx, `
			UPDATE sla_policies SET name=$3,description=NULLIF($4,''),calendar_id=$5,
				pause_states=$6,warning_threshold_percent=$7,escalation_actions=$8::jsonb,
				applies_to=$9::jsonb,enabled=COALESCE($10,enabled),updated_at=now()
			WHERE workspace_id=$1 AND id=$2
		`, workspaceID, id, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), calendar, input.PauseStates, input.WarningThresholdPercent, actions, applies, input.Enabled)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return ErrNotFound
		}
		if input.Targets != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM sla_policy_targets WHERE policy_id=$1`, id); err != nil {
				return err
			}
			for _, target := range input.Targets {
				if _, err := tx.Exec(ctx, `INSERT INTO sla_policy_targets(id,policy_id,priority,first_response_minutes,next_response_minutes,resolution_minutes) VALUES($1,$2,$3,$4,$5,$6)`, ids.New(ids.PrefixSLATarget), id, target.Priority, target.FirstResponseMinutes, target.NextResponseMinutes, target.ResolutionMinutes); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sla: update policy: %w", err)
	}
	return s.GetPolicy(ctx, workspaceID, id)
}

func (s *Service) holidays(ctx context.Context, calendarID string) ([]Holiday, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,date::text FROM calendar_holidays WHERE calendar_id=$1 ORDER BY date`, calendarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Holiday, 0)
	for rows.Next() {
		var item Holiday
		if err := rows.Scan(&item.ID, &item.Name, &item.Date); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Service) targets(ctx context.Context, policyID string) ([]Target, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,priority,first_response_minutes,next_response_minutes,resolution_minutes FROM sla_policy_targets WHERE policy_id=$1 ORDER BY priority`, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Target, 0)
	for rows.Next() {
		var item Target
		if err := rows.Scan(&item.ID, &item.Priority, &item.FirstResponseMinutes, &item.NextResponseMinutes, &item.ResolutionMinutes); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanCalendar(row scanner) (*CalendarRecord, error) {
	var item CalendarRecord
	var weekly []byte
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Timezone, &weekly, &item.IsDefault, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(weekly, &item.Weekly)
	return &item, nil
}
func scanPolicy(row scanner) (*Policy, error) {
	var item Policy
	var actions, applies []byte
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.CalendarID, &item.PauseStates, &item.WarningThresholdPercent, &actions, &applies, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(actions, &item.EscalationActions)
	_ = json.Unmarshal(applies, &item.AppliesTo)
	return &item, nil
}
func holidayDates(values []Holiday) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.Date
	}
	return result
}
