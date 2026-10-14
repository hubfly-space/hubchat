package analytics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
)

type DrilldownFilter struct{ GroupBy, InboxID, TeamID, Channel, TagID string }
type DrilldownRow struct {
	Key              string   `json:"key"`
	Label            string   `json:"label"`
	Current          int64    `json:"current"`
	Previous         int64    `json:"previous"`
	FirstResponseP50 *float64 `json:"first_response_p50"`
	FirstResponseP90 *float64 `json:"first_response_p90"`
	ResolutionP50    *float64 `json:"resolution_p50"`
	ResolutionP90    *float64 `json:"resolution_p90"`
}

// Drilldown compares equally sized creation cohorts. Percentiles include only
// threads with an observed response/resolution; unanswered threads are not zero.
func (s *Service) Drilldown(ctx context.Context, workspaceID string, from, to time.Time, filter DrilldownFilter) ([]DrilldownRow, error) {
	if !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		return nil, errors.New("analytics: choose a positive window of at most one year")
	}
	key, label := "c.channel", "c.channel"
	switch filter.GroupBy {
	case "", "channel":
	case "inbox":
		key = "c.inbox_id"
		label = "coalesce(i.name,'Unassigned')"
	case "team":
		key = "coalesce(c.team_id,'unassigned')"
		label = "coalesce(t.name,'Unassigned')"
	case "tag":
		key = "coalesce(tag.id,'untagged')"
		label = "coalesce(tag.name,'Untagged')"
	default:
		return nil, errors.New("analytics: group_by must be inbox, team, channel, or tag")
	}
	joinTags := ""
	if filter.GroupBy == "tag" {
		joinTags = "LEFT JOIN conversation_tags ct ON ct.conversation_id=c.id LEFT JOIN tags tag ON tag.id=ct.tag_id AND tag.workspace_id=c.workspace_id"
	}
	predicate, scopeArgs := authorization.InboxPredicate(ctx, "c.inbox_id", 9)
	args := append([]any{workspaceID, from, to, from.Add(-to.Sub(from)), filter.InboxID, filter.TeamID, filter.Channel, filter.TagID}, scopeArgs...)
	statement := fmt.Sprintf(`WITH measured AS (
 SELECT %s AS group_key,%s AS label,c.created_at,
 extract(epoch FROM (agent.at-customer.at))::float8 AS response_seconds,
 extract(epoch FROM (resolved.at-c.created_at))::float8 AS resolution_seconds
 FROM conversations c LEFT JOIN inboxes i ON i.id=c.inbox_id LEFT JOIN teams t ON t.id=c.team_id
 %s
 LEFT JOIN LATERAL(SELECT min(created_at) AS at FROM messages WHERE conversation_id=c.id AND kind='reply' AND author_type='customer') customer ON true
 LEFT JOIN LATERAL(SELECT min(created_at) AS at FROM messages WHERE conversation_id=c.id AND kind='reply' AND author_type IN ('agent','automation') AND created_at>=customer.at) agent ON true
 LEFT JOIN LATERAL(SELECT min(occurred_at) AS at FROM conversation_status_history WHERE conversation_id=c.id AND to_state='resolved') resolved ON true
 WHERE c.workspace_id=$1 AND c.created_at >= $4 AND c.created_at < $3
 AND ($5='' OR c.inbox_id=$5) AND ($6='' OR c.team_id=$6) AND ($7='' OR c.channel=$7)
 AND ($8='' OR EXISTS(SELECT 1 FROM conversation_tags selected WHERE selected.conversation_id=c.id AND selected.tag_id=$8))
 AND %s
 ) SELECT group_key,label,count(*) FILTER(WHERE created_at>=$2),count(*) FILTER(WHERE created_at<$2),
 percentile_cont(0.5) WITHIN GROUP(ORDER BY response_seconds) FILTER(WHERE created_at>=$2),
 percentile_cont(0.9) WITHIN GROUP(ORDER BY response_seconds) FILTER(WHERE created_at>=$2),
 percentile_cont(0.5) WITHIN GROUP(ORDER BY resolution_seconds) FILTER(WHERE created_at>=$2),
 percentile_cont(0.9) WITHIN GROUP(ORDER BY resolution_seconds) FILTER(WHERE created_at>=$2)
 FROM measured GROUP BY group_key,label ORDER BY count(*) FILTER(WHERE created_at>=$2) DESC,group_key LIMIT 200`, key, label, joinTags, predicate)
	rows, err := s.pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DrilldownRow{}
	for rows.Next() {
		var row DrilldownRow
		if err := rows.Scan(&row.Key, &row.Label, &row.Current, &row.Previous, &row.FirstResponseP50, &row.FirstResponseP90, &row.ResolutionP50, &row.ResolutionP90); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
