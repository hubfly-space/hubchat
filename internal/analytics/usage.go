package analytics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// UsageMetric is an observed workspace count. Used is a pointer because a
// counter that has never been recorded is different from a measured zero.
type UsageMetric struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Used     *int64 `json:"used"`
	Limit    *int64 `json:"limit,omitempty"`
	Unit     string `json:"unit,omitempty"`
	Period   string `json:"period,omitempty"`
	Measured bool   `json:"measured"`
}

type UsageLimit struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value int64  `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

type UsageSnapshot struct {
	ComputedAt    time.Time     `json:"computed_at"`
	MonthStart    time.Time     `json:"month_start"`
	DayStart      time.Time     `json:"day_start"`
	Metrics       []UsageMetric `json:"metrics"`
	RequestLimits []UsageLimit  `json:"request_limits"`
}

// Usage computes operational counters directly from workspace-owned tables.
// It intentionally does not manufacture values for counters that the runtime
// has not recorded yet, which keeps the self-hosted usage screen truthful.
func (s *Service) Usage(ctx context.Context, workspaceID string, now time.Time) (*UsageSnapshot, error) {
	if workspaceID == "" {
		return nil, errors.New("analytics: workspace id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	snapshot := &UsageSnapshot{
		ComputedAt:    now,
		MonthStart:    monthStart,
		DayStart:      dayStart,
		Metrics:       make([]UsageMetric, 0, 12),
		RequestLimits: make([]UsageLimit, 0),
	}

	counts := []struct {
		key, label, query, period string
	}{
		{"workspace_members", "Workspace members", `SELECT count(*) FROM workspace_members WHERE workspace_id=$1`, "lifetime"},
		{"teams", "Teams", `SELECT count(*) FROM teams WHERE workspace_id=$1`, "lifetime"},
		{"inboxes", "Inboxes", `SELECT count(*) FROM inboxes WHERE workspace_id=$1`, "lifetime"},
		{"widgets", "Widgets", `SELECT count(*) FROM widgets WHERE workspace_id=$1`, "lifetime"},
		{"portals", "Portals", `SELECT count(*) FROM portals WHERE workspace_id=$1`, "lifetime"},
		{"feedback_boards", "Feedback boards", `SELECT count(*) FROM feedback_boards WHERE workspace_id=$1`, "lifetime"},
		{"knowledge_bases", "Knowledge bases", `SELECT count(*) FROM knowledge_bases WHERE workspace_id=$1`, "lifetime"},
		{"monthly_active_contacts", "Monthly active contacts", `SELECT count(*) FROM customers WHERE workspace_id=$1 AND last_seen_at >= $2`, "month"},
		{"conversations_month", "Conversations this month", `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND created_at >= $2`, "month"},
		{"events_month", "Stored events this month", `SELECT count(*) FROM workspace_events WHERE workspace_id=$1 AND occurred_at >= $2`, "month"},
		{"storage_bytes", "Attachments", `SELECT COALESCE(sum(size_bytes),0) FROM files WHERE workspace_id=$1 AND committed_at IS NOT NULL`, "lifetime"},
	}
	for _, item := range counts {
		args := []any{workspaceID}
		if item.period == "month" {
			args = append(args, monthStart)
		}
		var value int64
		if err := s.pool.QueryRow(ctx, item.query, args...).Scan(&value); err != nil {
			return nil, fmt.Errorf("analytics: usage %s: %w", item.key, err)
		}
		unit := "count"
		if item.key == "storage_bytes" {
			unit = "bytes"
		}
		valueCopy := value
		snapshot.Metrics = append(snapshot.Metrics, UsageMetric{Key: item.key, Label: item.label, Used: &valueCopy, Unit: unit, Period: item.period, Measured: true})
	}

	// API request counters are optional. An installation that has not enabled
	// request metering gets an explicit unavailable value instead of a fake 0.
	var apiRequests int64
	err := s.pool.QueryRow(ctx, `SELECT value FROM usage_counters WHERE workspace_id=$1 AND metric='api_requests' AND period=$2`, workspaceID, dayStart).Scan(&apiRequests)
	if err == nil {
		snapshot.Metrics = append(snapshot.Metrics, UsageMetric{Key: "api_requests_day", Label: "API requests today", Used: &apiRequests, Unit: "count", Period: "day", Measured: true})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("analytics: usage api requests: %w", err)
	} else {
		snapshot.Metrics = append(snapshot.Metrics, UsageMetric{Key: "api_requests_day", Label: "API requests today", Unit: "count", Period: "day", Measured: false})
	}

	rows, err := s.pool.Query(ctx, `SELECT key,value FROM workspace_limits WHERE workspace_id=$1 AND value IS NOT NULL ORDER BY key`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("analytics: usage limits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value int64
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("analytics: scan usage limit: %w", err)
		}
		snapshot.RequestLimits = append(snapshot.RequestLimits, UsageLimit{Key: key, Label: key, Value: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics: usage limits rows: %w", err)
	}
	for index := range snapshot.Metrics {
		for _, limit := range snapshot.RequestLimits {
			if snapshot.Metrics[index].Key == limit.Key {
				value := limit.Value
				snapshot.Metrics[index].Limit = &value
			}
		}
	}
	return snapshot, nil
}
