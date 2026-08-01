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
	// Rank is kept for the global-search cursor. It is an internal sort key,
	// not a relevance score exposed as part of the public result contract.
	Rank float32
}

// SearchMessages runs a full-text query against message bodies, matching the
// messages_search index's expression exactly (migration 0002) so Postgres
// can use it rather than falling back to a sequential scan. Notes and public
// replies are both searched — an agent looking for "what did we already say
// about this" needs both — but redacted messages and event rows never are,
// the same exclusion the index itself encodes.
func (s *Service) SearchMessages(ctx context.Context, workspaceID, query string, limit int) ([]MessageSearchResult, error) {
	return s.SearchMessagesPage(ctx, workspaceID, query, 0, time.Time{}, "", false, limit)
}

// SearchMessagesPage returns full-text hits in stable relevance order. Rank
// alone is not a sufficient cursor because several messages can have the
// same score, so created_at and id provide deterministic tie-breakers.
func (s *Service) SearchMessagesPage(ctx context.Context, workspaceID, query string, beforeRank float32, beforeCreatedAt time.Time, beforeID string, hasCursor bool, limit int) ([]MessageSearchResult, error) {
	// 101 is allowed internally so the global search service can request one
	// look-ahead row for a public page of 100.
	if limit <= 0 || limit > 101 {
		limit = 20
	}
	return s.repo.searchMessagesPage(ctx, workspaceID, query, beforeRank, beforeCreatedAt, beforeID, hasCursor, limit)
}

func (r *repository) searchMessagesPage(ctx context.Context, workspaceID, query string, beforeRank float32, beforeCreatedAt time.Time, beforeID string, hasCursor bool, limit int) ([]MessageSearchResult, error) {
	args := []any{workspaceID, query}
	statement := `WITH hits AS (
		SELECT m.conversation_id, m.id, c.subject, m.author_name,
		       ts_headline('english', m.body, plainto_tsquery('english', $2),
		                   'MaxWords=20, MinWords=8, MaxFragments=1') AS snippet,
		       m.created_at,
		       ts_rank(to_tsvector('english', m.body), plainto_tsquery('english', $2)) AS search_rank
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.workspace_id = $1
		  AND m.kind <> 'event' AND m.redacted_at IS NULL
		  AND to_tsvector('english', m.body) @@ plainto_tsquery('english', $2)
	)
	SELECT conversation_id, id, subject, author_name, snippet, created_at, search_rank
	FROM hits`
	if hasCursor {
		statement += ` WHERE (search_rank,created_at,id) < ($3,$4,$5)`
		args = append(args, beforeRank, beforeCreatedAt, beforeID)
	}
	statement += fmt.Sprintf(` ORDER BY search_rank DESC,created_at DESC,id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("conversation: search messages: %w", err)
	}
	defer rows.Close()

	out := []MessageSearchResult{}
	for rows.Next() {
		var res MessageSearchResult
		if err := rows.Scan(
			&res.ConversationID, &res.MessageID, &res.ConversationSubj, &res.AuthorName,
			&res.Snippet, &res.CreatedAt, &res.Rank,
		); err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}
