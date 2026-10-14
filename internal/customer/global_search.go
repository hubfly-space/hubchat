package customer

import (
	"context"
	"github.com/hubchat/hubchat/internal/searchmodel"
)

func (s *Service) SearchCompanies(ctx context.Context, workspaceID, query, afterID string, limit int, filters searchmodel.Filters) ([]searchmodel.Match, error) {
	statement := `SELECT t.id,t.name,left(coalesce(t.domain::text,''),180) FROM companies t
 WHERE t.workspace_id=$1 AND (t.name || ' ' || coalesce(t.domain::text,'') ILIKE '%'||$2||'%' OR to_tsvector('english',t.name || ' ' || coalesce(t.domain::text,'')) @@ websearch_to_tsquery('english',$2)) AND ($3='' OR t.id>$3)
 AND ($5::timestamptz IS NULL OR t.created_at >= $5) AND ($6::timestamptz IS NULL OR t.created_at < $6)`
	args := []any{workspaceID, query, afterID, limit, filters.After, filters.Before, filters.Channel, filters.TagID}
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
		item := searchmodel.Match{Kind: "company"}
		if err := rows.Scan(&item.EntityID, &item.Title, &item.Snippet); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
