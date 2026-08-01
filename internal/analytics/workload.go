package analytics

import (
	"context"
	"fmt"
	"time"
)

// WorkloadRow is a deliberately descriptive operational snapshot. Counts are
// computed from workspace-owned records at request time so a report never
// presents a stale, silently manufactured agent score.
type WorkloadRow struct {
	SubjectType         string `json:"subject_type"`
	SubjectID           string `json:"subject_id"`
	Name                string `json:"name"`
	ActiveConversations int64  `json:"active_conversations"`
	ActiveTickets       int64  `json:"active_tickets"`
	RepliesSent         int64  `json:"replies_sent"`
	Resolved            int64  `json:"resolved"`
}

// Workload returns current queue ownership plus activity in the selected
// period. It is one query for members and one for teams, but both paths share
// the same workspace and time predicates so the two views remain comparable.
func (s *Service) Workload(ctx context.Context, workspaceID string, from, to time.Time) ([]WorkloadRow, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("analytics: workspace id is required")
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -30)
	}
	if !from.Before(to) {
		return nil, fmt.Errorf("analytics: from must be before to")
	}

	rows, err := s.pool.Query(ctx, `
		SELECT 'member',wm.id,u.name,
			(SELECT count(*) FROM conversations c WHERE c.workspace_id=$1 AND c.assignee_id=wm.id AND c.state NOT IN ('closed','spam')),
			(SELECT count(*) FROM tickets t WHERE t.workspace_id=$1 AND t.assignee_id=wm.id AND t.status NOT IN ('closed')),
			(SELECT count(*) FROM messages m WHERE m.workspace_id=$1 AND m.author_id=wm.id AND m.author_type IN ('agent','automation') AND m.kind='reply' AND m.created_at >= $2 AND m.created_at < $3),
			(SELECT count(*) FROM conversation_status_history h JOIN conversations c ON c.id=h.conversation_id AND c.workspace_id=$1 WHERE h.actor_id=wm.id AND h.to_state='resolved' AND h.occurred_at >= $2 AND h.occurred_at < $3)
			+ (SELECT count(*) FROM ticket_status_history h JOIN tickets t ON t.id=h.ticket_id AND t.workspace_id=$1 WHERE h.actor_id=wm.id AND h.to_status IN ('resolved','closed') AND h.occurred_at >= $2 AND h.occurred_at < $3)
		FROM workspace_members wm JOIN users u ON u.id=wm.user_id
		WHERE wm.workspace_id=$1
		UNION ALL
		SELECT 'team',t.id,t.name,
			(SELECT count(*) FROM conversations c WHERE c.workspace_id=$1 AND c.team_id=t.id AND c.state NOT IN ('closed','spam')),
			(SELECT count(*) FROM tickets tk WHERE tk.workspace_id=$1 AND tk.team_id=t.id AND tk.status NOT IN ('closed')),
			(SELECT count(*) FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.workspace_id=$1 WHERE m.workspace_id=$1 AND c.team_id=t.id AND m.author_type IN ('agent','automation') AND m.kind='reply' AND m.created_at >= $2 AND m.created_at < $3),
			(SELECT count(*) FROM conversation_status_history h JOIN conversations c ON c.id=h.conversation_id AND c.workspace_id=$1 WHERE c.team_id=t.id AND h.to_state='resolved' AND h.occurred_at >= $2 AND h.occurred_at < $3)
			+ (SELECT count(*) FROM ticket_status_history h JOIN tickets tk ON tk.id=h.ticket_id AND tk.workspace_id=$1 WHERE tk.team_id=t.id AND h.to_status IN ('resolved','closed') AND h.occurred_at >= $2 AND h.occurred_at < $3)
		FROM teams t
		WHERE t.workspace_id=$1
		ORDER BY 1,3,2
	`, workspaceID, from, to)
	if err != nil {
		return nil, fmt.Errorf("analytics: workload query: %w", err)
	}
	defer rows.Close()
	result := make([]WorkloadRow, 0)
	for rows.Next() {
		var item WorkloadRow
		if err := rows.Scan(&item.SubjectType, &item.SubjectID, &item.Name, &item.ActiveConversations, &item.ActiveTickets, &item.RepliesSent, &item.Resolved); err != nil {
			return nil, fmt.Errorf("analytics: scan workload: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analytics: workload rows: %w", err)
	}
	return result, nil
}
