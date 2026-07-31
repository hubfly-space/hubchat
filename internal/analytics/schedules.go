package analytics

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/jackc/pgx/v5"
)

const JobScheduledReports = "analytics.report_schedules"

var (
	ErrInvalidScheduleReport     = errors.New("analytics: report is required")
	ErrInvalidScheduleCadence    = errors.New("analytics: cadence must be daily, weekly, or monthly")
	ErrInvalidScheduleRecipients = errors.New("analytics: at least one valid recipient is required")
	ErrInvalidScheduleFormat     = errors.New("analytics: only csv schedules are supported")
	ErrInvalidScheduleOptions    = errors.New("analytics: schedule options are invalid")
)

type ReportSchedule struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	ReportID    string         `json:"report_id"`
	Cadence     string         `json:"cadence"`
	Options     map[string]any `json:"options"`
	Recipients  []string       `json:"recipients"`
	Format      string         `json:"format"`
	Enabled     bool           `json:"enabled"`
	LastSentAt  *time.Time     `json:"last_sent_at,omitempty"`
	NextRunAt   *time.Time     `json:"next_run_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type ScheduleInput struct {
	ReportID   string         `json:"report_id"`
	Cadence    string         `json:"cadence"`
	Options    map[string]any `json:"options"`
	Recipients []string       `json:"recipients"`
	Format     string         `json:"format"`
	Enabled    *bool          `json:"enabled"`
}

type emailQueue interface {
	Enqueue(context.Context, jobs.Spec) (string, error)
}

type scheduledReportEmail struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (s *Service) ListSchedules(ctx context.Context, workspaceID, reportID string) ([]ReportSchedule, error) {
	return s.ListSchedulesPage(ctx, workspaceID, reportID, time.Time{}, "", 0)
}

func (s *Service) ListSchedulesPage(ctx context.Context, workspaceID, reportID string, before time.Time, beforeID string, limit int) ([]ReportSchedule, error) {
	query := `SELECT id,workspace_id,report_id,cadence,options,recipients,format,enabled,last_sent_at,next_run_at,created_at
		FROM report_schedules WHERE workspace_id=$1`
	args := []any{workspaceID}
	if !before.IsZero() {
		query += " AND (created_at,id)<($" + strconv.Itoa(len(args)+1) + ", $" + strconv.Itoa(len(args)+2) + ")"
		args = append(args, before, beforeID)
	}
	if strings.TrimSpace(reportID) != "" {
		query += " AND report_id=$" + strconv.Itoa(len(args)+1)
		args = append(args, strings.TrimSpace(reportID))
	}
	query += " ORDER BY created_at DESC, id DESC"
	if limit > 0 {
		query += " LIMIT $" + strconv.Itoa(len(args)+1)
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("analytics: list schedules: %w", err)
	}
	defer rows.Close()
	result := make([]ReportSchedule, 0)
	for rows.Next() {
		item, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func (s *Service) CreateSchedule(ctx context.Context, workspaceID string, input ScheduleInput, now time.Time) (*ReportSchedule, error) {
	options, recipients, format, enabled, err := normalizeScheduleInput(input)
	if err != nil {
		return nil, err
	}
	reportID := strings.TrimSpace(input.ReportID)
	if reportID == "" {
		return nil, ErrInvalidScheduleReport
	}
	if _, err := s.GetReport(ctx, workspaceID, reportID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidScheduleReport
		}
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	next, err := nextScheduleRun(now, input.Cadence, options)
	if err != nil {
		return nil, err
	}
	if !enabled {
		next = nil
	}
	encoded, _ := json.Marshal(options)
	id := ids.New(ids.PrefixReportSchedule)
	_, err = s.pool.Exec(ctx, `INSERT INTO report_schedules(id,workspace_id,report_id,cadence,options,recipients,format,enabled,next_run_at)
		VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)`, id, workspaceID, reportID, strings.ToLower(strings.TrimSpace(input.Cadence)), encoded, recipients, format, enabled, next)
	if err != nil {
		return nil, fmt.Errorf("analytics: create schedule: %w", err)
	}
	return s.GetSchedule(ctx, workspaceID, id)
}

func (s *Service) GetSchedule(ctx context.Context, workspaceID, id string) (*ReportSchedule, error) {
	return scanSchedule(s.pool.QueryRow(ctx, `SELECT id,workspace_id,report_id,cadence,options,recipients,format,enabled,last_sent_at,next_run_at,created_at
		FROM report_schedules WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
}

func (s *Service) UpdateSchedule(ctx context.Context, workspaceID, id string, input ScheduleInput, now time.Time) (*ReportSchedule, error) {
	options, recipients, format, enabled, err := normalizeScheduleInput(input)
	if err != nil {
		return nil, err
	}
	reportID := strings.TrimSpace(input.ReportID)
	if reportID == "" {
		return nil, ErrInvalidScheduleReport
	}
	if _, err := s.GetReport(ctx, workspaceID, reportID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidScheduleReport
		}
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	next, err := nextScheduleRun(now, input.Cadence, options)
	if err != nil {
		return nil, err
	}
	if !enabled {
		next = nil
	}
	encoded, _ := json.Marshal(options)
	tag, err := s.pool.Exec(ctx, `UPDATE report_schedules SET report_id=$3,cadence=$4,options=$5::jsonb,recipients=$6,format=$7,enabled=$8,next_run_at=$9
		WHERE workspace_id=$1 AND id=$2`, workspaceID, id, reportID, strings.ToLower(strings.TrimSpace(input.Cadence)), encoded, recipients, format, enabled, next)
	if err != nil {
		return nil, fmt.Errorf("analytics: update schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	return s.GetSchedule(ctx, workspaceID, id)
}

func (s *Service) DeleteSchedule(ctx context.Context, workspaceID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM report_schedules WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("analytics: delete schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func normalizeScheduleInput(input ScheduleInput) (map[string]any, []string, string, bool, error) {
	cadence := strings.ToLower(strings.TrimSpace(input.Cadence))
	if cadence != "daily" && cadence != "weekly" && cadence != "monthly" {
		return nil, nil, "", false, ErrInvalidScheduleCadence
	}
	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "" {
		format = "csv"
	}
	if format != "csv" {
		return nil, nil, "", false, ErrInvalidScheduleFormat
	}
	recipients := make([]string, 0, len(input.Recipients))
	seen := make(map[string]struct{}, len(input.Recipients))
	for _, raw := range input.Recipients {
		address := strings.ToLower(strings.TrimSpace(raw))
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address {
			return nil, nil, "", false, ErrInvalidScheduleRecipients
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		recipients = append(recipients, address)
	}
	if len(recipients) == 0 {
		return nil, nil, "", false, ErrInvalidScheduleRecipients
	}
	options := map[string]any{}
	for key, value := range input.Options {
		options[key] = value
	}
	if err := validateScheduleOptions(cadence, options); err != nil {
		return nil, nil, "", false, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	return options, recipients, format, enabled, nil
}

func validateScheduleOptions(cadence string, options map[string]any) error {
	hour := numberOption(options, "hour", 9)
	if hour < 0 || hour > 23 {
		return ErrInvalidScheduleOptions
	}
	minute := numberOption(options, "minute", 0)
	if minute < 0 || minute > 59 {
		return ErrInvalidScheduleOptions
	}
	switch cadence {
	case "weekly":
		weekday := numberOption(options, "weekday", 1)
		if weekday < 0 || weekday > 6 {
			return ErrInvalidScheduleOptions
		}
	case "monthly":
		day := numberOption(options, "day", 1)
		if day < 1 || day > 28 {
			return ErrInvalidScheduleOptions
		}
	}
	return nil
}

func numberOption(options map[string]any, key string, fallback int) int {
	value, ok := options[key]
	if !ok {
		return fallback
	}
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := strconv.Atoi(number.String())
		return parsed
	default:
		return -1
	}
}

func nextScheduleRun(now time.Time, cadence string, options map[string]any) (*time.Time, error) {
	if err := validateScheduleOptions(cadence, options); err != nil {
		return nil, err
	}
	location := time.UTC
	if raw, ok := options["timezone"].(string); ok && strings.TrimSpace(raw) != "" {
		loaded, err := time.LoadLocation(raw)
		if err != nil {
			return nil, ErrInvalidScheduleOptions
		}
		location = loaded
	}
	local := now.In(location)
	hour := numberOption(options, "hour", 9)
	minute := numberOption(options, "minute", 0)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, location)
	switch cadence {
	case "daily":
		if !candidate.After(local) {
			candidate = candidate.AddDate(0, 0, 1)
		}
	case "weekly":
		weekday := time.Weekday(numberOption(options, "weekday", 1))
		days := (int(weekday) - int(candidate.Weekday()) + 7) % 7
		candidate = candidate.AddDate(0, 0, days)
		if !candidate.After(local) {
			candidate = candidate.AddDate(0, 0, 7)
		}
	case "monthly":
		day := numberOption(options, "day", 1)
		candidate = time.Date(local.Year(), local.Month(), day, hour, minute, 0, 0, location)
		if !candidate.After(local) {
			candidate = time.Date(local.Year(), local.Month()+1, day, hour, minute, 0, 0, location)
		}
	default:
		return nil, ErrInvalidScheduleCadence
	}
	result := candidate.UTC()
	return &result, nil
}

func scanSchedule(row interface{ Scan(...any) error }) (*ReportSchedule, error) {
	var item ReportSchedule
	var options []byte
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.ReportID, &item.Cadence, &options, &item.Recipients, &item.Format, &item.Enabled, &item.LastSentAt, &item.NextRunAt, &item.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(options, &item.Options); err != nil {
		return nil, fmt.Errorf("analytics: decode schedule options: %w", err)
	}
	return &item, nil
}

// ExportCSV emits only workspace-scoped rollups. A report definition may
// provide a metrics array; direct callers may provide a comma-separated list.
func (s *Service) ExportCSV(ctx context.Context, workspaceID, reportID string, metrics []string, from, to time.Time) ([]byte, error) {
	if strings.TrimSpace(reportID) != "" {
		report, err := s.GetReport(ctx, workspaceID, reportID)
		if err != nil {
			return nil, err
		}
		metrics = metricsFromDefinition(report.Definition)
	}
	clean := make([]string, 0, len(metrics))
	seen := map[string]struct{}{}
	for _, metric := range metrics {
		metric = strings.TrimSpace(metric)
		if metric == "" {
			continue
		}
		if _, ok := seen[metric]; ok {
			continue
		}
		seen[metric] = struct{}{}
		clean = append(clean, metric)
	}
	if len(clean) == 0 {
		return nil, errors.New("analytics: at least one metric is required")
	}
	sort.Strings(clean)
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{"metric", "grain", "bucket", "dimensions", "value", "count", "computed_at"}); err != nil {
		return nil, err
	}
	for _, metric := range clean {
		items, err := s.Rollups(ctx, workspaceID, metric, "day", from, to)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			dimensions, _ := json.Marshal(item.Dimensions)
			if err := writer.Write([]string{item.Metric, item.Grain, item.Bucket.Format(time.RFC3339), string(dimensions), strconv.FormatFloat(item.Value, 'f', -1, 64), strconv.Itoa(item.Count), item.ComputedAt.Format(time.RFC3339)}); err != nil {
				return nil, err
			}
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func metricsFromDefinition(definition map[string]any) []string {
	var metrics []string
	if values, ok := definition["metrics"].([]any); ok {
		for _, value := range values {
			if metric, ok := value.(string); ok {
				metrics = append(metrics, metric)
			}
		}
	}
	if metric, ok := definition["metric"].(string); ok {
		metrics = append(metrics, metric)
	}
	return metrics
}

// RunScheduledReports claims due schedules one at a time, renders their CSV,
// queues durable email jobs, and advances the next run. The claim update is
// committed before queueing so two schedulers cannot send the same cadence.
func (s *Service) RunScheduledReports(ctx context.Context, now time.Time, queue emailQueue) (int, error) {
	if queue == nil {
		return 0, errors.New("analytics: email queue is unavailable")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	processed := 0
	for {
		var item ReportSchedule
		var options []byte
		var reportName string
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return processed, err
		}
		err = tx.QueryRow(ctx, `SELECT schedules.id,schedules.workspace_id,schedules.report_id,schedules.cadence,schedules.options,schedules.recipients,schedules.format,schedules.enabled,schedules.last_sent_at,schedules.next_run_at,schedules.created_at,reports.name
			FROM report_schedules schedules JOIN saved_reports reports ON reports.id=schedules.report_id AND reports.workspace_id=schedules.workspace_id
			WHERE schedules.enabled AND schedules.next_run_at IS NOT NULL AND schedules.next_run_at <= $1
			ORDER BY schedules.next_run_at,schedules.id LIMIT 1 FOR UPDATE OF schedules SKIP LOCKED`, now).Scan(&item.ID, &item.WorkspaceID, &item.ReportID, &item.Cadence, &options, &item.Recipients, &item.Format, &item.Enabled, &item.LastSentAt, &item.NextRunAt, &item.CreatedAt, &reportName)
		if errors.Is(err, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			break
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return processed, err
		}
		_ = json.Unmarshal(options, &item.Options)
		next, nextErr := nextScheduleRun(now, item.Cadence, item.Options)
		if nextErr != nil {
			_ = tx.Rollback(ctx)
			return processed, nextErr
		}
		if _, err := tx.Exec(ctx, `UPDATE report_schedules SET last_sent_at=$3,next_run_at=$4 WHERE workspace_id=$1 AND id=$2`, item.WorkspaceID, item.ID, now, next); err != nil {
			_ = tx.Rollback(ctx)
			return processed, err
		}
		if err := tx.Commit(ctx); err != nil {
			return processed, err
		}

		body, err := s.ExportCSV(ctx, item.WorkspaceID, item.ReportID, nil, now.AddDate(0, 0, -30), now)
		if err != nil {
			return processed, err
		}
		for _, recipient := range item.Recipients {
			_, err := queue.Enqueue(ctx, jobs.Spec{WorkspaceID: item.WorkspaceID, Queue: "email", Type: "email.send", Payload: scheduledReportEmail{To: recipient, Subject: "Hubchat report: " + reportName, Body: string(body)}, DedupeKey: "report-schedule:" + item.ID + ":" + now.UTC().Format(time.RFC3339) + ":" + recipient})
			if errors.Is(err, jobs.ErrDuplicate) {
				continue
			}
			if err != nil {
				return processed, err
			}
		}
		processed++
	}
	return processed, nil
}
