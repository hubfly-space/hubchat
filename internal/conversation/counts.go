package conversation

import "context"

// Counts is the inbox sidebar's view badges in one round trip — computing
// these by loading every conversation client-side (or one list call per
// badge) is exactly the query pattern §17 warns against.
type Counts struct {
	All               int
	Unassigned        int
	Mine              int
	Following         int
	WaitingOnUs       int
	WaitingOnCustomer int
	Snoozed           int
	Resolved          int
	Spam              int
}

// Counts computes every sidebar badge for one workspace and viewer in a
// single query.
func (s *Service) Counts(ctx context.Context, workspaceID, memberID string) (*Counts, error) {
	return s.repo.counts(ctx, workspaceID, memberID)
}

func (r *repository) counts(ctx context.Context, workspaceID, memberID string) (*Counts, error) {
	var c Counts
	err := r.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE c.state NOT IN ('closed', 'resolved', 'spam')),
			count(*) FILTER (WHERE c.assignee_id IS NULL AND c.state NOT IN ('closed', 'spam')),
			count(*) FILTER (WHERE c.assignee_id = $2 AND c.state NOT IN ('closed', 'spam')),
			count(*) FILTER (WHERE cf.member_id IS NOT NULL AND c.state NOT IN ('closed', 'spam')),
			count(*) FILTER (WHERE c.state = 'waiting_for_support'),
			count(*) FILTER (WHERE c.state IN ('waiting_for_customer', 'pending')),
			count(*) FILTER (WHERE c.state = 'snoozed'),
			count(*) FILTER (WHERE c.state = 'resolved'),
			count(*) FILTER (WHERE c.state = 'spam')
		FROM conversations c
		LEFT JOIN conversation_followers cf ON cf.conversation_id = c.id AND cf.member_id = $2
		WHERE c.workspace_id = $1
	`, workspaceID, memberID).Scan(
		&c.All, &c.Unassigned, &c.Mine, &c.Following,
		&c.WaitingOnUs, &c.WaitingOnCustomer, &c.Snoozed, &c.Resolved, &c.Spam,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
