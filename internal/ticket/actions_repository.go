package ticket

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// validStatuses mirrors the CHECK constraint on tickets.status (migration 0005).
var validStatuses = map[string]bool{
	"new": true, "open": true, "pending": true, "on_hold": true, "resolved": true, "closed": true,
}

var validPriorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}

var validLinkRelations = map[string]bool{
	"related": true, "duplicate_of": true, "blocks": true, "blocked_by": true,
}

// lockAndLoad takes a row-level lock on the ticket for the duration of the
// caller's transaction, so a concurrent status change and this mutation
// cannot interleave into an inconsistent reopen_count/resolved_at pair.
func (r *repository) lockAndLoad(ctx context.Context, tx pgx.Tx, workspaceID, id string) (*Ticket, error) {
	row := tx.QueryRow(ctx, `SELECT `+ticketColumns+`
		FROM tickets WHERE workspace_id = $1 AND id = $2 FOR UPDATE
	`, workspaceID, id)
	return scanTicket(row)
}

// The tenancy checks below are read-only EXISTS queries against tables owned
// by other modules (workspace_members, teams, inboxes, customers, companies,
// tags) — the same pattern conversation/actions_repository.go uses. A
// caller-supplied id's foreign key only proves the row exists somewhere, not
// that it belongs to this workspace (§11.3), and an EXISTS check is cheap
// enough to make directly rather than routing every reference check through
// another module's service.
func (r *repository) memberInWorkspace(ctx context.Context, workspaceID, memberID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM workspace_members WHERE id = $1 AND workspace_id = $2)
	`, memberID, workspaceID).Scan(&exists)
	return exists, err
}

func (r *repository) teamInWorkspace(ctx context.Context, workspaceID, teamID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM teams WHERE id = $1 AND workspace_id = $2)
	`, teamID, workspaceID).Scan(&exists)
	return exists, err
}

func (r *repository) inboxInWorkspace(ctx context.Context, workspaceID, inboxID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM inboxes WHERE id = $1 AND workspace_id = $2)
	`, inboxID, workspaceID).Scan(&exists)
	return exists, err
}

func (r *repository) customerInWorkspace(ctx context.Context, workspaceID, customerID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM customers WHERE id = $1 AND workspace_id = $2)
	`, customerID, workspaceID).Scan(&exists)
	return exists, err
}

func (r *repository) companyInWorkspace(ctx context.Context, workspaceID, companyID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM companies WHERE id = $1 AND workspace_id = $2)
	`, companyID, workspaceID).Scan(&exists)
	return exists, err
}

func (r *repository) tagInWorkspace(ctx context.Context, workspaceID, tagID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tags WHERE id = $1 AND workspace_id = $2)
	`, tagID, workspaceID).Scan(&exists)
	return exists, err
}

// primaryCompanyID returns the first company linked to a customer, if any —
// used at ticket creation time to default a ticket's company from its
// customer when the caller did not name one explicitly. customerID is
// already workspace-scoped by the caller (customerInWorkspace), so this join
// needs no separate workspace predicate of its own.
func (r *repository) primaryCompanyID(ctx context.Context, customerID string) (*string, error) {
	var companyID string
	err := r.pool.QueryRow(ctx, `
		SELECT company_id FROM company_customers WHERE customer_id = $1
		ORDER BY company_id LIMIT 1
	`, customerID).Scan(&companyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &companyID, nil
}

func (r *repository) setAssignee(ctx context.Context, tx pgx.Tx, id string, assigneeID *string) error {
	_, err := tx.Exec(ctx, `UPDATE tickets SET assignee_id = $2, updated_at = now() WHERE id = $1`, id, assigneeID)
	return err
}

func (r *repository) setTeam(ctx context.Context, tx pgx.Tx, id string, teamID *string) error {
	_, err := tx.Exec(ctx, `UPDATE tickets SET team_id = $2, updated_at = now() WHERE id = $1`, id, teamID)
	return err
}

func (r *repository) setInbox(ctx context.Context, tx pgx.Tx, id string, inboxID *string) error {
	_, err := tx.Exec(ctx, `UPDATE tickets SET inbox_id = $2, updated_at = now() WHERE id = $1`, id, inboxID)
	return err
}

func (r *repository) setPriority(ctx context.Context, tx pgx.Tx, id, priority string) error {
	_, err := tx.Exec(ctx, `UPDATE tickets SET priority = $2, updated_at = now() WHERE id = $1`, id, priority)
	return err
}

func (r *repository) setCustomer(ctx context.Context, tx pgx.Tx, id string, customerID, companyID *string) error {
	_, err := tx.Exec(ctx, `
		UPDATE tickets SET customer_id = $2, company_id = $3, updated_at = now() WHERE id = $1
	`, id, customerID, companyID)
	return err
}

func (r *repository) setDueAt(ctx context.Context, tx pgx.Tx, id string, dueAt *time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE tickets SET due_at = $2, updated_at = now() WHERE id = $1`, id, dueAt)
	return err
}

// setStatus applies every timestamp/counter side effect a status transition
// carries — see Service.SetStatus for the rules that compute these values.
func (r *repository) setStatus(
	ctx context.Context, tx pgx.Tx, id, status string,
	firstResolvedAt, resolvedAt, closedAt *time.Time, reopenCount int,
) error {
	_, err := tx.Exec(ctx, `
		UPDATE tickets
		SET status = $2, first_resolved_at = $3, resolved_at = $4, closed_at = $5,
		    reopen_count = $6, updated_at = now()
		WHERE id = $1
	`, id, status, firstResolvedAt, resolvedAt, closedAt, reopenCount)
	return err
}

func (r *repository) insertStatusHistory(ctx context.Context, tx pgx.Tx, id, ticketID string, fromStatus, toStatus, actorType, actorID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ticket_status_history (id, ticket_id, from_status, to_status, actor_type, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, ticketID, nullIfEmpty(fromStatus), toStatus, actorType, nullIfEmpty(actorID))
	return err
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *repository) addTag(ctx context.Context, tx pgx.Tx, ticketID, tagID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ticket_tags (ticket_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, ticketID, tagID)
	return err
}

func (r *repository) removeTag(ctx context.Context, tx pgx.Tx, ticketID, tagID string) error {
	_, err := tx.Exec(ctx, `DELETE FROM ticket_tags WHERE ticket_id = $1 AND tag_id = $2`, ticketID, tagID)
	return err
}

func (r *repository) tagIDs(ctx context.Context, workspaceID, ticketID string) ([]string, error) {
	byTicket, err := r.tagIDsForMany(ctx, workspaceID, []string{ticketID})
	if err != nil {
		return nil, err
	}
	return byTicket[ticketID], nil
}

// tagIDsForMany loads tags for a whole page of tickets in one query — the
// list endpoint's alternative to one round trip per row.
func (r *repository) tagIDsForMany(ctx context.Context, workspaceID string, ticketIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(ticketIDs))
	if len(ticketIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT tt.ticket_id, tt.tag_id FROM ticket_tags tt
		JOIN tickets t ON t.id = tt.ticket_id
		WHERE t.workspace_id = $1 AND tt.ticket_id = ANY($2)
	`, workspaceID, ticketIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ticketID, tagID string
		if err := rows.Scan(&ticketID, &tagID); err != nil {
			return nil, err
		}
		out[ticketID] = append(out[ticketID], tagID)
	}
	return out, rows.Err()
}

func (r *repository) follow(ctx context.Context, tx pgx.Tx, ticketID, memberID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ticket_followers (ticket_id, member_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, ticketID, memberID)
	return err
}

func (r *repository) unfollow(ctx context.Context, tx pgx.Tx, ticketID, memberID string) error {
	_, err := tx.Exec(ctx, `DELETE FROM ticket_followers WHERE ticket_id = $1 AND member_id = $2`, ticketID, memberID)
	return err
}

func (r *repository) followerIDs(ctx context.Context, workspaceID, ticketID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tf.member_id FROM ticket_followers tf
		JOIN tickets t ON t.id = tf.ticket_id
		WHERE t.workspace_id = $1 AND tf.ticket_id = $2
	`, workspaceID, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// --------------------------------------------------------------- links

type TicketLink struct {
	ID       string
	SourceID string
	TargetID string
	Relation string
}

func (r *repository) addLink(ctx context.Context, tx pgx.Tx, id, workspaceID, sourceID, targetID, relation string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ticket_links (id, workspace_id, source_id, target_id, relation)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (source_id, target_id, relation) DO NOTHING
	`, id, workspaceID, sourceID, targetID, relation)
	return err
}

func (r *repository) removeLink(ctx context.Context, tx pgx.Tx, workspaceID, sourceID, targetID, relation string) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM ticket_links
		WHERE workspace_id = $1 AND source_id = $2 AND target_id = $3 AND relation = $4
	`, workspaceID, sourceID, targetID, relation)
	return err
}

// links returns every link touching ticketID, in either direction — a
// "blocked_by" recorded from the other side still belongs on this ticket's
// list.
func (r *repository) links(ctx context.Context, workspaceID, ticketID string) ([]TicketLink, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, source_id, target_id, relation FROM ticket_links
		WHERE workspace_id = $1 AND (source_id = $2 OR target_id = $2)
	`, workspaceID, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TicketLink{}
	for rows.Next() {
		var l TicketLink
		if err := rows.Scan(&l.ID, &l.SourceID, &l.TargetID, &l.Relation); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *repository) setParent(ctx context.Context, tx pgx.Tx, id string, parentID *string) error {
	_, err := tx.Exec(ctx, `UPDATE tickets SET parent_id = $2, updated_at = now() WHERE id = $1`, id, parentID)
	return err
}

// ancestorIDs walks parent_id from id upward, for cycle detection: a ticket
// cannot become its own ancestor's parent. Bounded at 64 hops so a corrupt
// chain (which the single-parent schema should never produce) cannot spin
// forever.
func (r *repository) ancestorIDs(ctx context.Context, workspaceID, id string) ([]string, error) {
	var out []string
	current := id
	for range 64 {
		var parentID *string
		err := r.pool.QueryRow(ctx, `
			SELECT parent_id FROM tickets WHERE workspace_id = $1 AND id = $2
		`, workspaceID, current).Scan(&parentID)
		if errors.Is(err, pgx.ErrNoRows) || parentID == nil {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, *parentID)
		current = *parentID
	}
	return out, nil
}

func (r *repository) childIDs(ctx context.Context, workspaceID, id string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM tickets WHERE workspace_id = $1 AND parent_id = $2
	`, workspaceID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			return nil, err
		}
		out = append(out, childID)
	}
	return out, rows.Err()
}

// duplicateCandidates surfaces other open tickets that plausibly describe
// the same issue — a deterministic rule (§6.3 "duplicate detection using
// deterministic matching rules"), not a fuzzy heuristic: the same customer or
// company, not yet resolved or closed, ranked by trigram title similarity
// against the pg_trgm extension already enabled for customer search. A
// similarity floor of 0.2 keeps unrelated tickets from a chatty customer out
// of the list.
func (r *repository) duplicateCandidates(ctx context.Context, workspaceID, excludeID, title string, customerID, companyID *string) ([]Ticket, error) {
	if customerID == nil && companyID == nil {
		return []Ticket{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+ticketColumns+`, similarity(title, $2) AS score
		FROM tickets
		WHERE workspace_id = $1
		  AND id <> $3
		  AND status NOT IN ('resolved', 'closed')
		  AND ((customer_id IS NOT NULL AND customer_id = $4) OR (company_id IS NOT NULL AND company_id = $5))
		  AND similarity(title, $2) > 0.2
		ORDER BY score DESC, created_at DESC
		LIMIT 5
	`, workspaceID, title, excludeID, customerID, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Ticket{}
	for rows.Next() {
		var score float32
		t, err := scanTicketWithScore(rows, &score)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// scanTicketWithScore scans a ticketColumns row plus one trailing similarity
// score column, letting duplicateCandidates order by it without adding a
// score field to Ticket itself.
func scanTicketWithScore(row interface{ Scan(dest ...any) error }, score *float32) (*Ticket, error) {
	var t Ticket
	err := row.Scan(
		&t.ID, &t.WorkspaceID, &t.Number, &t.Prefix, &t.Title, &t.Description, &t.Status, &t.Priority, &t.Type,
		&t.CustomerID, &t.CompanyID, &t.InboxID, &t.Channel, &t.AssigneeID, &t.TeamID, &t.ConversationID,
		&t.ParentID, &t.SLAPolicyID, &t.DueAt, &t.FirstResolvedAt, &t.ResolvedAt, &t.ClosedAt,
		&t.ReopenCount, &t.Version, &t.CreatedAt, &t.UpdatedAt, score,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func fkViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23503"
	}
	return false
}

func uniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
