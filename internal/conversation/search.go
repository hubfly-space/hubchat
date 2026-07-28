package conversation

import (
	"context"
	"fmt"
	"time"
)

// MessageSearchResult is one full-text hit, with enough conversation context
// that a search result can be rendered and linked to without a second
// lookup.
type MessageSearchResult struct {
	ConversationID   string
	MessageID        string
	ConversationSubj *string
	AuthorName       string
	Snippet          string
	CreatedAt        time.Time
}

// SearchMessages runs a full-text query against message bodies, matching the
// messages_search index's expression exactly (migration 0002) so Postgres
// can use it rather than falling back to a sequential scan. Notes and public
// replies are both searched — an agent looking for "what did we already say
// about this" needs both — but redacted messages and event rows never are,
// the same exclusion the index itself encodes.
func (s *Service) SearchMessages(ctx context.Context, workspaceID, query string, limit int) ([]MessageSearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.searchMessages(ctx, workspaceID, query, limit)
}

func (r *repository) searchMessages(ctx context.Context, workspaceID, query string, limit int) ([]MessageSearchResult, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT m.conversation_id, m.id, c.subject, m.author_name,
		       ts_headline('english', m.body, plainto_tsquery('english', $2),
		                   'MaxWords=20, MinWords=8, MaxFragments=1'),
		       m.created_at
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.workspace_id = $1
		  AND m.kind <> 'event' AND m.redacted_at IS NULL
		  AND to_tsvector('english', m.body) @@ plainto_tsquery('english', $2)
		ORDER BY ts_rank(to_tsvector('english', m.body), plainto_tsquery('english', $2)) DESC
		LIMIT $3
	`, workspaceID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("conversation: search messages: %w", err)
	}
	defer rows.Close()

	out := []MessageSearchResult{}
	for rows.Next() {
		var res MessageSearchResult
		if err := rows.Scan(
			&res.ConversationID, &res.MessageID, &res.ConversationSubj, &res.AuthorName,
			&res.Snippet, &res.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}
