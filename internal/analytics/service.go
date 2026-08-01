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

var (
	ErrInvalidReportName = errors.New("analytics: report name is required")
	ErrReportForbidden   = errors.New("analytics: report is not visible to this role")
)

type Service struct{ pool *database.Pool }
type MetricDefinition struct {
	Metric     string `json:"metric"`
	Label      string `json:"label"`
	Definition string `json:"definition"`
	Unit       string `json:"unit,omitempty"`
}

type Summary struct {
	ConversationsCreated  int64     `json:"conversations_created"`
	TicketsCreated        int64     `json:"tickets_created"`
	MessagesReceived      int64     `json:"messages_received"`
	MessagesSent          int64     `json:"messages_sent"`
	ConversationsResolved int64     `json:"conversations_resolved"`
	TicketsResolved       int64     `json:"tickets_resolved"`
	TicketsReopened       int64     `json:"tickets_reopened"`
	BacklogConversations  int64     `json:"backlog_conversations"`
	BacklogTickets        int64     `json:"backlog_tickets"`
	FirstResponseSeconds  float64   `json:"first_response_seconds"`
	NextResponseSeconds   float64   `json:"next_response_seconds"`
	ResolutionSeconds     float64   `json:"resolution_seconds"`
	SLACompliancePercent  float64   `json:"sla_compliance_percent"`
	SLAInstances          int64     `json:"sla_instances"`
	SLAMet                int64     `json:"sla_met"`
	SLABreached           int64     `json:"sla_breached"`
	ActiveSLAInstances    int64     `json:"active_sla_instances"`
	OpenSLABreached       int64     `json:"open_sla_breached"`
	SurveyResponses       int64     `json:"survey_responses"`
	CSATAverage           *float64  `json:"csat_average,omitempty"`
	CESAverage            *float64  `json:"ces_average,omitempty"`
	NPS                   *float64  `json:"nps,omitempty"`
	From                  time.Time `json:"from"`
	To                    time.Time `json:"to"`
	Timezone              string    `json:"timezone"`
	ComputedAt            time.Time `json:"computed_at"`
}
type Rollup struct {
	Metric     string         `json:"metric"`
	Grain      string         `json:"grain"`
	Bucket     time.Time      `json:"bucket"`
	Dimensions map[string]any `json:"dimensions"`
	Value      float64        `json:"value"`
	Count      int            `json:"count"`
	ComputedAt time.Time      `json:"computed_at"`
	// CursorKey preserves PostgreSQL's canonical jsonb text for stable
	// pagination when multiple dimensions share the same bucket. It is an
	// internal sort key and never appears in the public response.
	CursorKey string `json:"-"`
}
type SearchTerm struct {
	Query          string    `json:"query"`
	Count          int       `json:"count"`
	LastOccurredAt time.Time `json:"last_occurred_at"`
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

var metricDefinitions = []MetricDefinition{
	{Metric: "conversations.created", Label: "Conversations created", Definition: "Conversations created in the selected period, grouped by channel."},
	{Metric: "tickets.created", Label: "Tickets created", Definition: "Tickets created in the selected period."},
	{Metric: "messages.received", Label: "Messages received", Definition: "Customer replies recorded in the selected period."},
	{Metric: "messages.sent", Label: "Messages sent", Definition: "Agent and automation replies recorded in the selected period."},
	{Metric: "conversations.resolved", Label: "Conversations resolved", Definition: "Conversations transitioned to resolved in the selected period."},
	{Metric: "tickets.resolved", Label: "Tickets resolved", Definition: "Tickets transitioned to resolved or closed in the selected period."},
	{Metric: "support.first_response_seconds", Label: "First response", Definition: "Average elapsed wall-clock time from the first customer reply to the first agent reply in each conversation.", Unit: "seconds"},
	{Metric: "support.next_response_seconds", Label: "Next response", Definition: "Average elapsed wall-clock time from each customer reply to the next agent reply in the selected period.", Unit: "seconds"},
	{Metric: "support.resolution_seconds", Label: "Resolution time", Definition: "Average elapsed wall-clock time from ticket creation to its first resolution timestamp.", Unit: "seconds"},
	{Metric: "sla.compliance_percent", Label: "SLA compliance", Definition: "SLA instances satisfied divided by all met or breached instances in the selected period.", Unit: "percent"},
	{Metric: "support.backlog", Label: "Backlog", Definition: "Open conversations and tickets at the end of the selected period."},
	{Metric: "conversations.reopened", Label: "Conversations reopened", Definition: "Conversations moved from resolved back to an active state in the selected period."},
	{Metric: "tickets.reopened", Label: "Tickets reopened", Definition: "Tickets moved from resolved or closed back to an active state in the selected period."},
	{Metric: "forms.submitted", Label: "Forms submitted", Definition: "Form submissions recorded in the selected period."},
	{Metric: "sla.approaching", Label: "SLA warnings", Definition: "SLA instances that entered their warning window in the selected period."},
	{Metric: "sla.breached", Label: "SLA breaches", Definition: "SLA instances that breached their target in the selected period."},
	{Metric: "surfaces.widget.impressions", Label: "Widget impressions", Definition: "Widget mounts recorded by the visitor event channel in the selected period."},
	{Metric: "surfaces.widget.opens", Label: "Widget opens", Definition: "Widget panels opened by visitors in the selected period."},
	{Metric: "surfaces.widget.articles_viewed", Label: "Widget article views", Definition: "Knowledge-base articles opened inside the widget in the selected period."},
	{Metric: "surfaces.widget.conversations_started", Label: "Widget conversations", Definition: "Conversations started from the widget in the selected period."},
	{Metric: "surfaces.portal.conversations_started", Label: "Portal conversations", Definition: "Conversations started from the portal in the selected period."},
	{Metric: "knowledgebase.article_views", Label: "Article views", Definition: "Published knowledge-base article views recorded by the portal or widget surface in the selected period."},
	{Metric: "knowledgebase.searches", Label: "Knowledge-base searches", Definition: "Public knowledge-base searches recorded in the selected period."},
	{Metric: "knowledgebase.search_no_results", Label: "No-result searches", Definition: "Public knowledge-base searches that returned no articles in the selected period."},
	{Metric: "knowledgebase.article_helpful", Label: "Helpful article votes", Definition: "Positive helpfulness responses recorded for published articles in the selected period."},
	{Metric: "knowledgebase.article_unhelpful", Label: "Unhelpful article votes", Definition: "Negative helpfulness responses recorded for published articles in the selected period."},
	{Metric: "feedback.items_created", Label: "Feedback items created", Definition: "Feedback items submitted in the selected period."},
	{Metric: "feedback.votes", Label: "Feedback votes", Definition: "Customer votes recorded on feedback items in the selected period."},
	{Metric: "feedback.status_changed", Label: "Feedback status changes", Definition: "Feedback status transitions recorded in the selected period, grouped by destination status."},
	{Metric: "surveys.responses", Label: "Survey responses", Definition: "Completed survey responses recorded in the selected period."},
	{Metric: "surveys.csat", Label: "CSAT", Definition: "Average score from completed CSAT survey responses in the selected period."},
	{Metric: "surveys.ces", Label: "CES", Definition: "Average score from completed CES survey responses in the selected period."},
	{Metric: "surveys.nps", Label: "NPS", Definition: "Promoters minus detractors for completed NPS responses in the selected period, on the standard -100 to 100 scale."},
}

func (s *Service) MetricDefinitions() []MetricDefinition {
	result := make([]MetricDefinition, len(metricDefinitions))
	copy(result, metricDefinitions)
	return result
}

func (s *Service) Summary(ctx context.Context, workspaceID string, from, to time.Time) (*Summary, error) {
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -30)
	}
	if !from.Before(to) {
		return nil, errors.New("analytics: from must be before to")
	}
	result := &Summary{From: from, To: to, Timezone: "UTC", ComputedAt: time.Now().UTC()}
	queries := []struct {
		dest  *int64
		query string
		args  []any
	}{
		{&result.ConversationsCreated, `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND created_at >= $2 AND created_at < $3`, []any{workspaceID, from, to}},
		{&result.TicketsCreated, `SELECT count(*) FROM tickets WHERE workspace_id=$1 AND created_at >= $2 AND created_at < $3`, []any{workspaceID, from, to}},
		{&result.MessagesReceived, `SELECT count(*) FROM messages WHERE workspace_id=$1 AND kind='reply' AND author_type='customer' AND created_at >= $2 AND created_at < $3`, []any{workspaceID, from, to}},
		{&result.MessagesSent, `SELECT count(*) FROM messages WHERE workspace_id=$1 AND kind='reply' AND author_type IN ('agent','automation') AND created_at >= $2 AND created_at < $3`, []any{workspaceID, from, to}},
		{&result.ConversationsResolved, `SELECT count(*) FROM conversation_status_history h JOIN conversations c ON c.id=h.conversation_id AND c.workspace_id=$1 WHERE h.to_state='resolved' AND h.occurred_at >= $2 AND h.occurred_at < $3`, []any{workspaceID, from, to}},
		{&result.TicketsResolved, `SELECT count(*) FROM ticket_status_history h JOIN tickets t ON t.id=h.ticket_id AND t.workspace_id=$1 WHERE h.to_status IN ('resolved','closed') AND h.occurred_at >= $2 AND h.occurred_at < $3`, []any{workspaceID, from, to}},
		{&result.TicketsReopened, `SELECT count(*) FROM ticket_status_history h JOIN tickets t ON t.id=h.ticket_id AND t.workspace_id=$1 WHERE h.from_status IN ('resolved','closed') AND h.to_status NOT IN ('resolved','closed') AND h.occurred_at >= $2 AND h.occurred_at < $3`, []any{workspaceID, from, to}},
		{&result.BacklogConversations, `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND created_at < $2 AND state NOT IN ('resolved','closed','spam')`, []any{workspaceID, to}},
		{&result.BacklogTickets, `SELECT count(*) FROM tickets WHERE workspace_id=$1 AND created_at < $2 AND status NOT IN ('resolved','closed')`, []any{workspaceID, to}},
	}
	for _, item := range queries {
		if err := s.pool.QueryRow(ctx, item.query, item.args...).Scan(item.dest); err != nil {
			return nil, fmt.Errorf("analytics: summary count: %w", err)
		}
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(avg(extract(epoch FROM (reply.created_at - first_message.created_at))),0)::float8
		FROM (
			SELECT conversation_id, min(created_at) AS created_at
			FROM messages
			WHERE workspace_id=$1 AND kind='reply' AND author_type='customer' AND created_at >= $2 AND created_at < $3
			GROUP BY conversation_id
		) first_message
		JOIN LATERAL (
			SELECT created_at FROM messages m
			WHERE m.workspace_id=$1 AND m.conversation_id=first_message.conversation_id AND m.kind='reply' AND m.author_type IN ('agent','automation') AND m.created_at > first_message.created_at
			ORDER BY m.created_at, m.id LIMIT 1
		) reply ON true
	`, workspaceID, from, to).Scan(&result.FirstResponseSeconds); err != nil {
		return nil, fmt.Errorf("analytics: first response: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(avg(extract(epoch FROM (reply.created_at - customer_message.created_at))),0)::float8
		FROM messages customer_message
		JOIN LATERAL (
			SELECT created_at FROM messages m
			WHERE m.workspace_id=$1 AND m.conversation_id=customer_message.conversation_id
			  AND m.kind='reply' AND m.author_type IN ('agent','automation')
			  AND m.created_at > customer_message.created_at
			ORDER BY m.created_at, m.id LIMIT 1
		) reply ON true
		WHERE customer_message.workspace_id=$1
		  AND customer_message.kind='reply' AND customer_message.author_type='customer'
		  AND customer_message.created_at >= $2 AND customer_message.created_at < $3
	`, workspaceID, from, to).Scan(&result.NextResponseSeconds); err != nil {
		return nil, fmt.Errorf("analytics: next response: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(avg(extract(epoch FROM (first_resolved_at - created_at))),0)::float8
		FROM tickets WHERE workspace_id=$1 AND first_resolved_at >= $2 AND first_resolved_at < $3 AND first_resolved_at IS NOT NULL
	`, workspaceID, from, to).Scan(&result.ResolutionSeconds); err != nil {
		return nil, fmt.Errorf("analytics: resolution: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE state='met'), count(*) FILTER (WHERE state='breached')
		FROM sla_instances WHERE workspace_id=$1 AND COALESCE(satisfied_at, breached_at) >= $2 AND COALESCE(satisfied_at, breached_at) < $3
	`, workspaceID, from, to).Scan(&result.SLAMet, &result.SLABreached); err != nil {
		return nil, fmt.Errorf("analytics: sla: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE state='active'), count(*) FILTER (WHERE state='breached')
		FROM sla_instances WHERE workspace_id=$1
	`, workspaceID).Scan(&result.ActiveSLAInstances, &result.OpenSLABreached); err != nil {
		return nil, fmt.Errorf("analytics: current sla: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE r.submitted_at IS NOT NULL),
			avg(r.score) FILTER (WHERE r.submitted_at IS NOT NULL AND s.type='csat'),
			avg(r.score) FILTER (WHERE r.submitted_at IS NOT NULL AND s.type='ces'),
			(
				count(*) FILTER (WHERE r.submitted_at IS NOT NULL AND s.type='nps' AND r.score >= 9) -
				count(*) FILTER (WHERE r.submitted_at IS NOT NULL AND s.type='nps' AND r.score <= 6)
			)::double precision * 100 / NULLIF(count(*) FILTER (WHERE r.submitted_at IS NOT NULL AND s.type='nps'), 0)
		FROM survey_responses r
		JOIN surveys s ON s.id=r.survey_id AND s.workspace_id=r.workspace_id
		WHERE r.workspace_id=$1 AND r.submitted_at >= $2 AND r.submitted_at < $3
	`, workspaceID, from, to).Scan(&result.SurveyResponses, &result.CSATAverage, &result.CESAverage, &result.NPS); err != nil {
		return nil, fmt.Errorf("analytics: survey aggregates: %w", err)
	}
	result.SLAInstances = result.SLAMet + result.SLABreached
	if result.SLAInstances > 0 {
		result.SLACompliancePercent = float64(result.SLAMet) * 100 / float64(result.SLAInstances)
	}
	return result, nil
}

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
	records := make([]events.Record, 0)
	for rows.Next() {
		var record events.Record
		if err := rows.Scan(&record.ID, &record.Sequence, &record.Type, &record.OccurredAt, &record.Data); err != nil {
			rows.Close()
			return 0, err
		}
		last = record.Sequence
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	// Drain the cursor before writing rollups. pgx keeps the transaction's
	// connection busy while rows are open, so attempting an upsert inside the
	// scan loop fails with "conn busy" on PostgreSQL.
	count := 0
	for _, record := range records {
		metrics := metricsForEvent(record)
		if len(metrics) == 0 {
			continue
		}
		bucket := time.Date(record.OccurredAt.UTC().Year(), record.OccurredAt.UTC().Month(), record.OccurredAt.UTC().Day(), 0, 0, 0, 0, time.UTC)
		for _, metric := range metrics {
			encoded, err := json.Marshal(metric.dimensions)
			if err != nil {
				return count, err
			}
			if _, err := tx.Exec(ctx, `
			INSERT INTO report_rollups(id,workspace_id,metric,grain,bucket,dimensions,value,count,computed_at)
			VALUES($1,$2,$3,'day',$4,$5::jsonb,1,1,$6)
			ON CONFLICT (workspace_id,metric,grain,bucket,dimensions) DO UPDATE SET
				value=report_rollups.value+1,count=report_rollups.count+1,computed_at=EXCLUDED.computed_at`,
				ids.New(ids.PrefixRollup), workspaceID, metric.name, bucket, encoded, now); err != nil {
				return count, err
			}
			count++
		}
	}
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
	metrics := metricsForEvent(record)
	if len(metrics) == 0 {
		return "", nil, false
	}
	return metrics[0].name, metrics[0].dimensions, true
}

type eventMetric struct {
	name       string
	dimensions map[string]any
}

func metricsForEvent(record events.Record) []eventMetric {
	var data struct {
		Channel     string `json:"channel"`
		AuthorType  string `json:"author_type"`
		Type        string `json:"type"`
		Surface     string `json:"surface"`
		ResultCount int    `json:"result_count"`
		Helpful     bool   `json:"helpful"`
		To          string `json:"to"`
	}
	_ = json.Unmarshal(record.Data, &data)
	dimensions := map[string]any{}
	switch record.Type {
	case events.ConversationCreated:
		if data.Channel != "" {
			dimensions["channel"] = data.Channel
		}
		result := []eventMetric{{name: "conversations.created", dimensions: dimensions}}
		if data.Channel == "widget" || data.Channel == "portal" {
			result = append(result, eventMetric{name: "surfaces." + data.Channel + ".conversations_started", dimensions: map[string]any{}})
		}
		return result
	case events.TicketCreated:
		return []eventMetric{{name: "tickets.created", dimensions: dimensions}}
	case events.MessageCreated:
		if data.AuthorType == "customer" {
			return []eventMetric{{name: "messages.received", dimensions: dimensions}}
		}
		return []eventMetric{{name: "messages.sent", dimensions: dimensions}}
	case events.ConversationResolved:
		return []eventMetric{{name: "conversations.resolved", dimensions: dimensions}}
	case events.ConversationStateSet:
		var state struct{ From, To string }
		_ = json.Unmarshal(record.Data, &state)
		if state.From == "resolved" && state.To != "resolved" {
			return []eventMetric{{name: "conversations.reopened", dimensions: dimensions}}
		}
		return nil
	case events.TicketStateSet:
		var state struct{ From, To string }
		_ = json.Unmarshal(record.Data, &state)
		result := []eventMetric{}
		if state.To == "resolved" || state.To == "closed" {
			result = append(result, eventMetric{name: "tickets.resolved", dimensions: dimensions})
		}
		if (state.From == "resolved" || state.From == "closed") && state.To != "resolved" && state.To != "closed" {
			result = append(result, eventMetric{name: "tickets.reopened", dimensions: dimensions})
		}
		return result
	case events.FeedbackCreated:
		return []eventMetric{{name: "feedback.items_created", dimensions: dimensions}}
	case events.FeedbackVoteRecorded:
		return []eventMetric{{name: "feedback.votes", dimensions: dimensions}}
	case events.FeedbackStatusChanged:
		if data.To != "" {
			dimensions["status"] = data.To
		}
		return []eventMetric{{name: "feedback.status_changed", dimensions: dimensions}}
	case events.ArticleViewed:
		if data.Surface != "" {
			dimensions["surface"] = data.Surface
		}
		return []eventMetric{{name: "knowledgebase.article_views", dimensions: dimensions}}
	case events.ArticleFeedbackRecorded:
		if data.Helpful {
			return []eventMetric{{name: "knowledgebase.article_helpful", dimensions: dimensions}}
		}
		return []eventMetric{{name: "knowledgebase.article_unhelpful", dimensions: dimensions}}
	case events.ArticleSearchRecorded:
		if data.Surface != "" {
			dimensions["surface"] = data.Surface
		}
		result := []eventMetric{{name: "knowledgebase.searches", dimensions: dimensions}}
		if data.ResultCount == 0 {
			result = append(result, eventMetric{name: "knowledgebase.search_no_results", dimensions: dimensions})
		}
		return result
	case events.SurveyResponseCreated:
		return []eventMetric{{name: "surveys.responses", dimensions: dimensions}}
	case events.FormSubmitted:
		return []eventMetric{{name: "forms.submitted", dimensions: dimensions}}
	case events.SLAApproaching:
		return []eventMetric{{name: "sla.approaching", dimensions: dimensions}}
	case events.SLABreached:
		return []eventMetric{{name: "sla.breached", dimensions: dimensions}}
	case events.EventReceived:
		// Customer/visitor application events deliberately enter the shared
		// event log under one stable envelope type. Only this closed set is
		// promoted to report metrics; arbitrary application events remain
		// available to automation and the developer event stream without
		// silently becoming analytics dimensions.
		switch data.Type {
		case "widget.impression":
			return []eventMetric{{name: "surfaces.widget.impressions", dimensions: dimensions}}
		case "widget.opened":
			return []eventMetric{{name: "surfaces.widget.opens", dimensions: dimensions}}
		case "widget.article_viewed":
			return []eventMetric{{name: "surfaces.widget.articles_viewed", dimensions: dimensions}}
		}
		return nil
	default:
		return nil
	}
}
func (s *Service) Rollups(ctx context.Context, workspaceID, metric, grain string, from, to time.Time) ([]Rollup, error) {
	result := make([]Rollup, 0)
	var afterBucket time.Time
	afterDimensions := ""
	for {
		page, err := s.RollupsPage(ctx, workspaceID, metric, grain, from, to, afterBucket, afterDimensions, 200)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if len(page) < 200 {
			return result, nil
		}
		last := page[len(page)-1]
		afterBucket = last.Bucket
		afterDimensions = last.CursorKey
	}
}

// RollupsPage returns rollups in stable bucket/dimension order. A bucket can
// contain several dimension series, so bucket alone is not a sufficient
// cursor; PostgreSQL's canonical jsonb text is the deterministic tie-breaker.
func (s *Service) RollupsPage(ctx context.Context, workspaceID, metric, grain string, from, to, afterBucket time.Time, afterDimensions string, limit int) ([]Rollup, error) {
	if grain == "" {
		grain = "day"
	}
	if limit <= 0 || limit > 201 {
		limit = 200
	}
	query := `SELECT metric,grain,bucket,dimensions::text,value,count,computed_at FROM report_rollups WHERE workspace_id=$1 AND metric=$2 AND grain=$3`
	args := []any{workspaceID, metric, grain}
	if !from.IsZero() {
		query += ` AND bucket >= $` + fmt.Sprint(len(args)+1)
		args = append(args, from)
	}
	if !to.IsZero() {
		query += ` AND bucket < $` + fmt.Sprint(len(args)+1)
		args = append(args, to)
	}
	if !afterBucket.IsZero() || afterDimensions != "" {
		query += ` AND (bucket,dimensions::text) > ($` + fmt.Sprint(len(args)+1) + `,$` + fmt.Sprint(len(args)+2) + `)`
		args = append(args, afterBucket, afterDimensions)
	}
	query += ` ORDER BY bucket ASC,dimensions::text ASC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Rollup, 0)
	for rows.Next() {
		var item Rollup
		var dimensions string
		if err := rows.Scan(&item.Metric, &item.Grain, &item.Bucket, &dimensions, &item.Value, &item.Count, &item.ComputedAt); err != nil {
			return nil, err
		}
		item.CursorKey = dimensions
		_ = json.Unmarshal([]byte(dimensions), &item.Dimensions)
		result = append(result, item)
	}
	return result, rows.Err()
}

// NoResultSearchesPage returns the most frequent exact no-result queries. It
// is intentionally raw operational data: Hubchat does not classify or infer
// intent from customer search text.
func (s *Service) NoResultSearchesPage(ctx context.Context, workspaceID string, from, to time.Time, afterCount int, after time.Time, afterQuery string, limit int) ([]SearchTerm, error) {
	if limit <= 0 || limit > 201 {
		limit = 50
	}
	args := []any{workspaceID, from, to}
	query := `WITH terms AS (
		SELECT query,count(*)::int AS count,max(occurred_at) AS last_occurred_at
		FROM article_searches
		WHERE workspace_id=$1 AND result_count=0 AND query<>'' AND occurred_at >= $2 AND occurred_at < $3
		GROUP BY query
	)
	SELECT query,count,last_occurred_at FROM terms`
	if afterCount > 0 || !after.IsZero() || afterQuery != "" {
		query += ` WHERE (count,last_occurred_at,query) < ($4,$5,$6)`
		args = append(args, afterCount, after, afterQuery)
	}
	query += ` ORDER BY count DESC,last_occurred_at DESC,query DESC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]SearchTerm, 0)
	for rows.Next() {
		var item SearchTerm
		if err := rows.Scan(&item.Query, &item.Count, &item.LastOccurredAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) ListReports(ctx context.Context, workspaceID string) ([]Report, error) {
	return s.ListReportsPage(ctx, workspaceID, "", "", 200)
}

// ListReportsPage returns saved reports in stable name/id order. The cursor
// uses the same two fields so reports with identical names are not skipped.
func (s *Service) ListReportsPage(ctx context.Context, workspaceID, beforeName, beforeID string, limit int) ([]Report, error) {
	return s.listReportsPage(ctx, workspaceID, "", "", true, beforeName, beforeID, limit)
}

// ListReportsPageForActor applies the report's optional role visibility to a
// member-facing list. Owners retain access to every report in their workspace;
// other roles see reports explicitly shared with their role, reports owned by
// them, and reports with no visibility restriction.
func (s *Service) ListReportsPageForActor(ctx context.Context, workspaceID, role, memberID, beforeName, beforeID string, limit int) ([]Report, error) {
	return s.listReportsPage(ctx, workspaceID, role, memberID, false, beforeName, beforeID, limit)
}

func (s *Service) listReportsPage(ctx context.Context, workspaceID, role, memberID string, includeAll bool, beforeName, beforeID string, limit int) ([]Report, error) {
	if limit <= 0 || limit > 201 {
		limit = 200
	}
	query := `SELECT id,workspace_id,name,coalesce(description,''),definition,date_range,coalesce(timezone,''),visible_to_roles,owner_id,created_at,updated_at FROM saved_reports WHERE workspace_id=$1`
	args := []any{workspaceID, includeAll, role, memberID}
	query += ` AND ($2::boolean OR cardinality(visible_to_roles)=0 OR $3=ANY(visible_to_roles) OR owner_id=$4)`
	if beforeName != "" || beforeID != "" {
		query += fmt.Sprintf(` AND (name,id) > ($%d,$%d)`, len(args)+1, len(args)+2)
		args = append(args, beforeName, beforeID)
	}
	query += ` ORDER BY name ASC,id ASC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
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

// GetReportForActor returns a report only when the caller can see it. The
// workspace predicate remains in GetReport, so a foreign id is never revealed
// as a visibility decision.
func (s *Service) GetReportForActor(ctx context.Context, workspaceID, role, memberID, id string) (*Report, error) {
	item, err := s.GetReport(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if !reportVisibleToActor(item, role, memberID) {
		return nil, ErrReportForbidden
	}
	return item, nil
}

func reportVisibleToActor(item *Report, role, memberID string) bool {
	if item == nil || role == "owner" || len(item.VisibleToRoles) == 0 || item.OwnerID != nil && *item.OwnerID == memberID {
		return item != nil
	}
	for _, visibleRole := range item.VisibleToRoles {
		if visibleRole == role {
			return true
		}
	}
	return false
}
func (s *Service) CreateReport(ctx context.Context, workspaceID, ownerID string, input ReportInput) (*Report, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidReportName
	}
	if input.DateRange == "" {
		input.DateRange = "last_30_days"
	}
	definition, _ := json.Marshal(input.Definition)
	visibleToRoles := input.VisibleToRoles
	if visibleToRoles == nil {
		visibleToRoles = []string{}
	}
	var owner any
	if ownerID != "" {
		owner = ownerID
	}
	id := ids.New(ids.PrefixSavedReport)
	_, err := s.pool.Exec(ctx, `INSERT INTO saved_reports(id,workspace_id,name,description,definition,date_range,timezone,visible_to_roles,owner_id) VALUES($1,$2,$3,NULLIF($4,''),$5::jsonb,$6,NULLIF($7,''),$8,$9)`, id, workspaceID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), definition, input.DateRange, strings.TrimSpace(input.Timezone), visibleToRoles, owner)
	if err != nil {
		return nil, err
	}
	return s.GetReport(ctx, workspaceID, id)
}
func (s *Service) GetReport(ctx context.Context, workspaceID, id string) (*Report, error) {
	item, err := scanReport(s.pool.QueryRow(ctx, `SELECT id,workspace_id,name,coalesce(description,''),definition,date_range,coalesce(timezone,''),visible_to_roles,owner_id,created_at,updated_at FROM saved_reports WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	return item, err
}

// UpdateReport replaces the saved report definition within one workspace.
// Schedules remain attached to the stable report id and therefore continue to
// use the new definition on their next run.
func (s *Service) UpdateReport(ctx context.Context, workspaceID, id string, input ReportInput) (*Report, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidReportName
	}
	if input.DateRange == "" {
		input.DateRange = "last_30_days"
	}
	definition, _ := json.Marshal(input.Definition)
	visibleToRoles := input.VisibleToRoles
	if visibleToRoles == nil {
		visibleToRoles = []string{}
	}
	tag, err := s.pool.Exec(ctx, `UPDATE saved_reports SET name=$3,description=NULLIF($4,''),definition=$5::jsonb,date_range=$6,timezone=NULLIF($7,''),visible_to_roles=$8,updated_at=now() WHERE workspace_id=$1 AND id=$2`,
		workspaceID, id, strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), definition, input.DateRange, strings.TrimSpace(input.Timezone), visibleToRoles)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	return s.GetReport(ctx, workspaceID, id)
}

// DeleteReport removes a workspace-owned saved report and its schedules. The
// caller must still provide an explicit UI confirmation before reaching this
// route; the service keeps the tenant predicate as the final boundary.
func (s *Service) DeleteReport(ctx context.Context, workspaceID, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM saved_reports WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
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
