package conversation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// validStates mirrors the CHECK constraint on conversations.state (migration
// 0002). "snoozed" is deliberately excluded: it only ever gets set alongside
// a wake time, through snooze below, never through the generic setState path.
var validStates = map[string]bool{
	"new": true, "open": true, "pending": true, "waiting_for_customer": true,
	"waiting_for_support": true, "resolved": true, "closed": true, "spam": true,
}

var validPriorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}

// lockAndLoadFull is lockAndLoad plus the fields the mutation methods below
// need to decide whether a change is a no-op and to record accurate history.
func (r *repository) lockAndLoadFull(ctx context.Context, tx pgx.Tx, workspaceID, id string) (*Conversation, error) {
	var c Conversation
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, inbox_id, channel, subject, state, priority,
		       customer_id, assignee_id, team_id, message_count,
		       last_message_preview, last_message_at, created_at
		FROM conversations
		WHERE workspace_id = $1 AND id = $2
		FOR UPDATE
	`, workspaceID, id).Scan(
		&c.ID, &c.WorkspaceID, &c.InboxID, &c.Channel, &c.Subject, &c.State, &c.Priority,
		&c.CustomerID, &c.AssigneeID, &c.TeamID, &c.MessageCount,
		&c.LastMessagePreview, &c.LastMessageAt, &c.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// memberInWorkspace and teamInWorkspace guard assignment: the FK on
// conversations.assignee_id/team_id only proves the row exists somewhere, not
// that it belongs to this tenant, and a workspace-scoped check is what §11.3
// requires before an id supplied by the caller is trusted.
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

func (r *repository) tagInWorkspace(ctx context.Context, workspaceID, tagID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tags WHERE id = $1 AND workspace_id = $2)
	`, tagID, workspaceID).Scan(&exists)
	return exists, err
}

func (r *repository) setAssignee(ctx context.Context, tx pgx.Tx, id string, assigneeID *string) error {
	_, err := tx.Exec(ctx, `UPDATE conversations SET assignee_id = $2 WHERE id = $1`, id, assigneeID)
	return err
}

func (r *repository) setTeam(ctx context.Context, tx pgx.Tx, id string, teamID *string) error {
	_, err := tx.Exec(ctx, `UPDATE conversations SET team_id = $2 WHERE id = $1`, id, teamID)
	return err
}

func (r *repository) setInbox(ctx context.Context, tx pgx.Tx, id, inboxID string) error {
	_, err := tx.Exec(ctx, `UPDATE conversations SET inbox_id = $2 WHERE id = $1`, id, inboxID)
	return err
}

func (r *repository) setPriority(ctx context.Context, tx pgx.Tx, id, priority string) error {
	_, err := tx.Exec(ctx, `UPDATE conversations SET priority = $2 WHERE id = $1`, id, priority)
	return err
}

func (r *repository) setState(ctx context.Context, tx pgx.Tx, id, state string) error {
	_, err := tx.Exec(ctx, `
		UPDATE conversations SET state = $2, snoozed_until = NULL WHERE id = $1
	`, id, state)
	return err
}

func (r *repository) snooze(ctx context.Context, tx pgx.Tx, id string, until time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE conversations SET state = 'snoozed', snoozed_until = $2 WHERE id = $1
	`, id, until)
	return err
}

// wokenConversation is the minimal shape WakeSnoozed needs to publish an
// event per conversation it wakes — just enough to route the event to the
// right workspace's subscribers.
type wokenConversation struct {
	ID          string
	WorkspaceID string
}

// wakeSnoozed reopens every conversation whose snooze has elapsed, using the
// conversations_snoozed partial index (migration 0002) — the scheduler's
// wake-up query it was built for. One statement, so there is no window
// between "find the expired ones" and "reopen them" for a second scheduler
// tick to double-wake the same row.
func (r *repository) wakeSnoozed(ctx context.Context) ([]wokenConversation, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE conversations
		SET state = 'open', snoozed_until = NULL
		WHERE state = 'snoozed' AND snoozed_until IS NOT NULL AND snoozed_until <= now()
		RETURNING id, workspace_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []wokenConversation{}
	for rows.Next() {
		var w wokenConversation
		if err := rows.Scan(&w.ID, &w.WorkspaceID); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// insertStatusHistory is the append-only record time-in-state reporting
// derives from (§13 conventions) — never updated, only ever added to.
func (r *repository) insertStatusHistory(
	ctx context.Context, tx pgx.Tx, id, conversationID, fromState, toState, actorType, actorID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO conversation_status_history
			(id, conversation_id, from_state, to_state, actor_type, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, id, conversationID, nullIfEmpty(fromState), toState, actorType, nullIfEmpty(actorID))
	return err
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *repository) addTag(ctx context.Context, tx pgx.Tx, conversationID, tagID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO conversation_tags (conversation_id, tag_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, conversationID, tagID)
	return err
}

func (r *repository) removeTag(ctx context.Context, tx pgx.Tx, conversationID, tagID string) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM conversation_tags WHERE conversation_id = $1 AND tag_id = $2
	`, conversationID, tagID)
	return err
}

func (r *repository) tagIDs(ctx context.Context, workspaceID, conversationID string) ([]string, error) {
	byConv, err := r.tagIDsForMany(ctx, workspaceID, []string{conversationID})
	if err != nil {
		return nil, err
	}
	return byConv[conversationID], nil
}

// tagIDsForMany loads tags for a whole page of conversations in one query —
// the list endpoint's per-row alternative would be N+1 round trips for a
// list that is otherwise a single query.
func (r *repository) tagIDsForMany(ctx context.Context, workspaceID string, conversationIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(conversationIDs))
	if len(conversationIDs) == 0 {
		return out, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT ct.conversation_id, ct.tag_id FROM conversation_tags ct
		JOIN conversations c ON c.id = ct.conversation_id
		WHERE c.workspace_id = $1 AND ct.conversation_id = ANY($2)
	`, workspaceID, conversationIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var conversationID, tagID string
		if err := rows.Scan(&conversationID, &tagID); err != nil {
			return nil, err
		}
		out[conversationID] = append(out[conversationID], tagID)
	}
	return out, rows.Err()
}

func (r *repository) follow(ctx context.Context, tx pgx.Tx, conversationID, memberID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO conversation_followers (conversation_id, member_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, conversationID, memberID)
	return err
}

func (r *repository) unfollow(ctx context.Context, tx pgx.Tx, conversationID, memberID string) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM conversation_followers WHERE conversation_id = $1 AND member_id = $2
	`, conversationID, memberID)
	return err
}

func (r *repository) followerIDs(ctx context.Context, workspaceID, conversationID string) ([]string, error) {
	var out []string
	var before string
	for {
		page, err := r.followerIDsPage(ctx, workspaceID, conversationID, before, 201)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < 201 {
			return out, nil
		}
		before = page[len(page)-1]
	}
}

func (r *repository) followerIDsPage(ctx context.Context, workspaceID, conversationID, before string, limit int) ([]string, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := "c.workspace_id = $1 AND cf.conversation_id = $2"
	args := []any{workspaceID, conversationID}
	if before != "" {
		where += " AND cf.member_id > $3"
		args = append(args, before)
	}
	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", len(args))
	rows, err := r.pool.Query(ctx, `
		SELECT cf.member_id FROM conversation_followers cf
		JOIN conversations c ON c.id = cf.conversation_id
		WHERE `+where+`
		ORDER BY cf.member_id ASC
		LIMIT `+limitPlaceholder, args...)
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

// latestMessageID is used by MarkRead: read state tracks the newest message a
// reader has seen, not every individual message, so this is the only id it
// needs.
func (r *repository) latestMessageID(ctx context.Context, workspaceID, conversationID string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		SELECT m.id FROM messages m
		WHERE m.workspace_id = $1 AND m.conversation_id = $2
		ORDER BY m.sequence DESC LIMIT 1
	`, workspaceID, conversationID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (r *repository) markRead(ctx context.Context, tx pgx.Tx, messageID, readerType, readerID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO message_reads (message_id, reader_type, reader_id) VALUES ($1, $2, $3)
		ON CONFLICT (message_id, reader_type, reader_id) DO UPDATE SET read_at = now()
	`, messageID, readerType, readerID)
	return err
}

// isRead reports whether readerID has already read the latest message —
// what Conversation.unread negates.
func (r *repository) isRead(ctx context.Context, workspaceID, conversationID, readerType, readerID string) (bool, error) {
	byConv, err := r.isReadForMany(ctx, workspaceID, []string{conversationID}, readerType, readerID)
	if err != nil {
		return false, err
	}
	return byConv[conversationID], nil
}

// isReadForMany answers "has readerID read the latest message" for a whole
// page of conversations in two queries total rather than two per row: the
// newest message id per conversation, then which of those ids this reader has
// a receipt for.
func (r *repository) isReadForMany(
	ctx context.Context, workspaceID string, conversationIDs []string, readerType, readerID string,
) (map[string]bool, error) {
	out := make(map[string]bool, len(conversationIDs))
	if len(conversationIDs) == 0 {
		return out, nil
	}

	rows, err := r.pool.Query(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (m.conversation_id) m.conversation_id, m.id AS message_id
			FROM messages m
			WHERE m.workspace_id = $1 AND m.conversation_id = ANY($2)
			ORDER BY m.conversation_id, m.sequence DESC
		)
		SELECT latest.conversation_id,
		       EXISTS (
		           SELECT 1 FROM message_reads mr
		           WHERE mr.message_id = latest.message_id AND mr.reader_type = $3 AND mr.reader_id = $4
		       ) AS read
		FROM latest
	`, workspaceID, conversationIDs, readerType, readerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var conversationID string
		var read bool
		if err := rows.Scan(&conversationID, &read); err != nil {
			return nil, err
		}
		out[conversationID] = read
	}
	// A conversation with no messages at all (should not happen — Start
	// always writes one) is treated as read rather than left ambiguous.
	for _, id := range conversationIDs {
		if _, ok := out[id]; !ok {
			out[id] = true
		}
	}
	return out, rows.Err()
}

func fkViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23503"
	}
	return false
}
