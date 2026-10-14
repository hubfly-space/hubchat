package ticket

import (
	"context"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/searchmodel"
)

func (s *Service) SearchMatches(ctx context.Context, workspaceID, query, afterID string, limit int, filters searchmodel.Filters) ([]searchmodel.Match, error) {
	statement := `SELECT t.id,t.prefix || '-' || t.number::text || ': ' || t.title,left(t.description,180) FROM tickets t
 WHERE t.workspace_id=$1 AND ((t.title || ' ' || t.description ILIKE '%'||$2||'%' OR to_tsvector('english',t.title || ' ' || t.description) @@ websearch_to_tsquery('english',$2)) OR lower(t.prefix||'-'||t.number::text)=lower($2) OR ('#'||t.number::text)=$2) AND ($3='' OR t.id>$3)
 AND ($5::timestamptz IS NULL OR t.created_at >= $5) AND ($6::timestamptz IS NULL OR t.created_at < $6) AND ($7='' OR t.channel=$7) AND ($8='' OR EXISTS(SELECT 1 FROM ticket_tags tt WHERE tt.ticket_id=t.id AND tt.tag_id=$8))`
	args := []any{workspaceID, query, afterID, limit, filters.After, filters.Before, filters.Channel, filters.TagID}
	predicate, scopeArgs := authorization.InboxPredicate(ctx, "t.inbox_id", 9)
	statement += " AND " + predicate
	args = append(args, scopeArgs...)
	statement += " ORDER BY t.id ASC LIMIT $4"
	// Unused filter parameters still need explicit types for PostgreSQL prepare.
	statement = `WITH filtered AS (` + statement + `) SELECT * FROM filtered WHERE ($7::text IS NOT NULL AND $8::text IS NOT NULL)`
	rows, err := s.pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []searchmodel.Match{}
	for rows.Next() {
		item := searchmodel.Match{Kind: "ticket"}
		if err := rows.Scan(&item.EntityID, &item.Title, &item.Snippet); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
