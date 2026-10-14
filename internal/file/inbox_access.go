package file

import "context"

// VisibleToInboxes protects direct file URLs as well as their parent routes.
func (s *Service) VisibleToInboxes(ctx context.Context, record *Record, inboxIDs []string) (bool, error) {
	if inboxIDs == nil {
		return true, nil
	}
	var query string
	switch record.OwnerType {
	case "conversation":
		query = `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2 AND inbox_id=ANY($3::text[]))`
	case "message":
		query = `SELECT EXISTS(SELECT 1 FROM messages m JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id WHERE m.workspace_id=$1 AND m.id=$2 AND c.inbox_id=ANY($3::text[]))`
	case "ticket":
		query = `SELECT EXISTS(SELECT 1 FROM tickets WHERE workspace_id=$1 AND id=$2 AND inbox_id=ANY($3::text[]))`
	case "form_submission":
		query = `SELECT EXISTS(SELECT 1 FROM form_submissions f WHERE f.workspace_id=$1 AND f.id=$2 AND ((f.result_type='conversation' AND EXISTS(SELECT 1 FROM conversations c WHERE c.workspace_id=$1 AND c.id=f.result_id AND c.inbox_id=ANY($3::text[]))) OR (f.result_type='ticket' AND EXISTS(SELECT 1 FROM tickets t WHERE t.workspace_id=$1 AND t.id=f.result_id AND t.inbox_id=ANY($3::text[])))))`
	default:
		return true, nil
	}
	var visible bool
	err := s.pool.QueryRow(ctx, query, record.WorkspaceID, record.OwnerID, inboxIDs).Scan(&visible)
	return visible, err
}
