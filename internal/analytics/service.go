package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var ErrInvalidReportName = errors.New("analytics: report name is required")

type Service struct{ pool *database.Pool }
type Rollup struct {
	Metric     string         `json:"metric"`
	Grain      string         `json:"grain"`
	Bucket     time.Time      `json:"bucket"`
	Dimensions map[string]any `json:"dimensions"`
	Value      float64        `json:"value"`
	Count      int            `json:"count"`
	ComputedAt time.Time      `json:"computed_at"`
}
type Report struct {
	ID             string         `json:"id"`
	WorkspaceID    string         `json:"workspace_id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Definition     map[string]any `json:"definition"`
	DateRange      string         `json:"date_range"`
	Timezone       string         `json:"timezone,omitempty"`
	VisibleToRoles []string       `json:"visible_to_roles"`
	OwnerID        *string        `json:"owner_id,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}
type ReportInput struct {
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Definition     map[string]any `json:"definition"`
	DateRange      string         `json:"date_range"`
	Timezone       string         `json:"timezone"`
	VisibleToRoles []string       `json:"visible_to_roles"`
}

func New(pool *database.Pool) *Service { return &Service{pool: pool} }

const JobRollup = "analytics.rollup"

// FoldAll incrementally folds the append-only event log for every workspace.
// The state row and each bucket upsert share one transaction, so a retry after
// a worker lease expiry cannot double-count an event.
func (s *Service) FoldAll(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM workspaces ORDER BY id`)
	if err != nil {
		return 0, err
	}
	var workspaces []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		workspaces = append(workspaces, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	total := 0
	for _, workspaceID := range workspaces {
		count, err := s.FoldWorkspace(ctx, workspaceID, now)
		if err != nil {
			return total, err
		}
		total += count
	}
	return total, nil
}

func (s *Service) FoldWorkspace(ctx context.Context, workspaceID string, now time.Time) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "analytics:"+workspaceID); err != nil {
		return 0, err
	}
	var after int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(last_sequence,0) FROM report_rollup_state WHERE workspace_id=$1 AND metric='events' AND grain='day'`, workspaceID).Scan(&after); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT id,sequence,type,occurred_at,data FROM workspace_events WHERE workspace_id=$1 AND sequence>$2 ORDER BY sequence`, workspaceID, after)
	if err != nil {
		return 0, err
	}
	var last int64 = after
	count := 0
	for rows.Next() {
		var record events.Record
		if err := rows.Scan(&record.ID, &record.Sequence, &record.Type, &record.OccurredAt, &record.Data); err != nil {
			rows.Close()
			return count, err
		}
		last = record.Sequence
		metric, dimensions, ok := metricForEvent(record)
		if !ok {
			continue
		}
		bucket := time.Date(record.OccurredAt.UTC().Year(), record.OccurredAt.UTC().Month(), record.OccurredAt.UTC().Day(), 0, 0, 0, 0, time.UTC)
		encoded, err := json.Marshal(dimensions)
		if err != nil {
			rows.Close()
			return count, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO report_rollups(id,workspace_id,metric,grain,bucket,dimensions,value,count,computed_at)
			VALUES($1,$2,$3,'day',$4,$5::jsonb,1,1,$6)
			ON CONFLICT (workspace_id,metric,grain,bucket,dimensions) DO UPDATE SET
				value=report_rollups.value+1,count=report_rollups.count+1,computed_at=EXCLUDED.computed_at`,
			ids.New(ids.PrefixRollup), workspaceID, metric, bucket, encoded, now); err != nil {
			rows.Close()
			return count, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return count, err
	}
	rows.Close()
	if last > after {
		if _, err := tx.Exec(ctx, `
			INSERT INTO report_rollup_state(workspace_id,metric,grain,last_sequence,last_bucket,updated_at)
			VALUES($1,'events','day',$2,$3,$4)
			ON CONFLICT (workspace_id,metric,grain) DO UPDATE SET last_sequence=EXCLUDED.last_sequence,last_bucket=EXCLUDED.last_bucket,updated_at=EXCLUDED.updated_at`, workspaceID, last, now, now); err != nil {
			return count, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return count, err
	}
	return count, nil
}

func metricForEvent(record events.Record) (string, map[string]any, bool) {
	var data struct {
		Channel    string `json:"channel"`
		AuthorType string `json:"author_type"`
	}
	_ = json.Unmarshal(record.Data, &data)
	dimensions := map[string]any{}
	switch record.Type {
	case events.ConversationCreated:
		if data.Channel != "" {
			dimensions["channel"] = data.Channel
		}
		return "conversations.created", dimensions, true
	case events.TicketCreated:
		return "tickets.created", dimensions, true
	case events.MessageCreated:
		if data.AuthorType == "customer" {
			return "messages.received", dimensions, true
		}
		return "messages.sent", dimensions, true
	case events.FeedbackCreated:
		return "feedback.created", dimensions, true
	case events.SurveyResponseCreated:
		return "surveys.responses", dimensions, true
	case events.FormSubmitted:
		return "forms.submitted", dimensions, true
	case events.SLAApproaching:
		return "sla.approaching", dimensions, true
	case events.SLABreached:
		return "sla.breached", dimensions, true
	default:
		return "", nil, false
	}
}
func (s *Service) Rollups(ctx context.Context, workspaceID, metric, grain string, from, to time.Time) ([]Rollup, error) {
	if grain == "" {
		grain = "day"
	}
	query := `SELECT metric,grain,bucket,dimensions,value,count,computed_at FROM report_rollups WHERE workspace_id=$1 AND metric=$2 AND grain=$3`
	args := []any{workspaceID, metric, grain}
	if !from.IsZero() {
		query += ` AND bucket >= $` + fmt.Sprint(len(args)+1)
		args = append(args, from)
	}
	if !to.IsZero() {
		query += ` AND bucket < $` + fmt.Sprint(len(args)+1)
		args = append(args, to)
	}
	query += ` ORDER BY bucket`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Rollup, 0)
	for rows.Next() {
		var item Rollup
		var dimensions []byte
		if err := rows.Scan(&item.Metric, &item.Grain, &item.Bucket, &dimensions, &item.Value, &item.Count, &item.ComputedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(dimensions, &item.Dimensions)
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Service) ListReports(ctx context.Context, workspaceID string) ([]Report, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,workspace_id,name,coalesce(description,''),definition,date_range,coalesce(timezone,''),visible_to_roles,owner_id,created_at,updated_at FROM saved_reports WHERE workspace_id=$1 ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Report, 0)
	for rows.Next() {
		item, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}
func (s *Service) CreateReport(ctx context.Context, workspaceID, ownerID string, input ReportInput) (*Report, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidReportName
	}
	if input.DateRange == "" {
		input.DateRange = "last_30_days"
	}
	definition, _ := json.Marshal(input.Definition)
	var owner any
	if ownerID != "" {
		owner = ownerID
	}
	id := ids.New(ids.PrefixSavedReport)
	_, err := s.pool.Exec(ctx, `INSERT INTO saved_reports(id,workspace_id,name,description,definition,date_range,timezone,visible_to_roles,owner_id) VALUES($1,$2,$3,NULLIF($4,''),$5::jsonb,$6,NULLIF($7,''),$8,$9)`, id, workspaceID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), definition, input.DateRange, strings.TrimSpace(input.Timezone), input.VisibleToRoles, owner)
	if err != nil {
		return nil, err
	}
	return s.GetReport(ctx, workspaceID, id)
}
func (s *Service) GetReport(ctx context.Context, workspaceID, id string) (*Report, error) {
	item, err := scanReport(s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,coalesce(description,''),definition,date_range,coalesce(timezone,''),visible_to_roles,owner_id,created_at,updated_at FROM saved_reports WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	return item, err
}
func scanReport(row interface{ Scan(...any) error }) (*Report, error) {
	var item Report
	var definition []byte
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &definition, &item.DateRange, &item.Timezone, &item.VisibleToRoles, &item.OwnerID, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(definition, &item.Definition)
	return &item, nil
}
