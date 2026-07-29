package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
)

var ErrNotFound = errors.New("conversation: not found")

type Conversation struct {
	ID                 string
	WorkspaceID        string
	InboxID            string
	Channel            string
	Subject            *string
	State              string
	Priority           string
	CustomerID         *string
	VisitorID          *string
	AssigneeID         *string
	TeamID             *string
	TicketID           *string
	MessageCount       int
	LastMessagePreview string
	LastMessageAt      time.Time
	LastCustomerAt     *time.Time
	SnoozedUntil       *time.Time
	CreatedAt          time.Time
}

// conversationColumns is the column list every full-row Conversation query
// shares, kept in one place so byID, List, and the mutation methods that
// reload after a write can never drift from each other's scan order.
const conversationColumns = `
	id, workspace_id, inbox_id, channel, subject, state, priority,
	customer_id, visitor_id, assignee_id, team_id, ticket_id, message_count,
	last_message_preview, last_message_at, last_customer_at, snoozed_until, created_at
`

func scanConversation(row interface{ Scan(dest ...any) error }) (*Conversation, error) {
	var c Conversation
	err := row.Scan(
		&c.ID, &c.WorkspaceID, &c.InboxID, &c.Channel, &c.Subject, &c.State, &c.Priority,
		&c.CustomerID, &c.VisitorID, &c.AssigneeID, &c.TeamID, &c.TicketID, &c.MessageCount,
		&c.LastMessagePreview, &c.LastMessageAt, &c.LastCustomerAt, &c.SnoozedUntil, &c.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

type Message struct {
	ID              string
	ClientID        *string
	ConversationID  string
	WorkspaceID     string
	Kind            string
	AuthorType      string
	AuthorID        *string
	AuthorName      string
	Body            string
	QuotedMessageID *string
	Delivery        string
	Sequence        int64
	EditedAt        *time.Time
	RedactedAt      *time.Time
	CreatedAt       time.Time
}

// messageColumns is the column list every message-row query shares.
const messageColumns = `
	id, client_id, conversation_id, workspace_id, kind, author_type,
	author_id, author_name, body, quoted_message_id, delivery, sequence,
	edited_at, redacted_at, created_at
`

func scanMessage(row interface{ Scan(dest ...any) error }) (*Message, error) {
	var m Message
	err := row.Scan(
		&m.ID, &m.ClientID, &m.ConversationID, &m.WorkspaceID, &m.Kind, &m.AuthorType,
		&m.AuthorID, &m.AuthorName, &m.Body, &m.QuotedMessageID, &m.Delivery, &m.Sequence,
		&m.EditedAt, &m.RedactedAt, &m.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

type repository struct {
	pool *database.Pool
}

// insert writes the conversation row inside the caller's transaction.
//
// Taking tx rather than reaching for the pool is what makes Start's guarantee
// real: a conversation and its opening message commit together, or neither
// does. Using the pool here would leave an orphaned, message-less conversation
// behind whenever the message insert failed.
func (r *repository) insert(
	ctx context.Context,
	tx pgx.Tx,
	id, workspaceID, inboxID, channel string,
	subject *string,
	customerID *string,
	visitorID *string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO conversations
			(id, workspace_id, inbox_id, channel, subject, customer_id, visitor_id, state, priority)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'new', 'normal')
	`, id, workspaceID, inboxID, channel, subject, customerID, visitorID)
	return err
}

// idsForVisitor lists every conversation a visitor has ever started, for
// building the realtime grant their WebSocket connection needs (§9): a
// visitor may resume all of their own threads, never just the most recent.
func (r *repository) idsForVisitor(ctx context.Context, workspaceID, visitorID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM conversations WHERE workspace_id = $1 AND visitor_id = $2
	`, workspaceID, visitorID)
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

func (r *repository) byID(ctx context.Context, workspaceID, id string) (*Conversation, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+conversationColumns+`
		FROM conversations WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	return scanConversation(row)
}

// lockConversation takes a row-level lock on the conversation for the
// duration of the caller's transaction. Must be called before insertMessage
// so that sequence allocation there — max(sequence)+1 — is computed against a
// consistent view even when two sends race on the same conversation. Without
// this lock, two concurrent inserts can both compute the same "next" sequence
// and collide (or worse, silently interleave) instead of one waiting for the
// other.
func (r *repository) lockConversation(ctx context.Context, tx pgx.Tx, workspaceID, id string) error {
	var discard string
	err := tx.QueryRow(ctx, `
		SELECT id FROM conversations WHERE workspace_id = $1 AND id = $2 FOR UPDATE
	`, workspaceID, id).Scan(&discard)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// lockAndLoad takes the same lock as lockConversation and returns the fields
// the caller needs, in one round trip.
//
// PostMessage needs the inbox id for the event payload it publishes. Reading
// it separately would be a second query for a row already locked, and reading
// it before the lock would race the very update the lock exists to serialise.
func (r *repository) lockAndLoad(ctx context.Context, tx pgx.Tx, workspaceID, id string) (*Conversation, error) {
	var conv Conversation
	err := tx.QueryRow(ctx, `
		SELECT id, workspace_id, inbox_id, channel, state, priority
		FROM conversations
		WHERE workspace_id = $1 AND id = $2
		FOR UPDATE
	`, workspaceID, id).Scan(
		&conv.ID, &conv.WorkspaceID, &conv.InboxID,
		&conv.Channel, &conv.State, &conv.Priority,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &conv, nil
}

// insertMessage allocates the next sequence number and inserts the message in
// one round trip, returning the row it wrote — including the sequence, which
// the caller cannot know in advance. Callers must hold the lock from
// lockConversation first.
func (r *repository) insertMessage(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	clientID *string,
	conversationID, workspaceID, kind, authorType string,
	authorID *string,
	authorName, body string,
) (*Message, error) {
	row := tx.QueryRow(ctx, `
		WITH next_seq AS (
			SELECT coalesce(max(sequence), 0) + 1 AS seq
			FROM messages
			WHERE conversation_id = $1
		)
		INSERT INTO messages
			(id, client_id, conversation_id, workspace_id, kind, author_type,
			 author_id, author_name, body, sequence)
		SELECT $2, $3, $1, $4, $5, $6, $7, $8, $9, next_seq.seq
		FROM next_seq
		RETURNING `+messageColumns+`
	`, conversationID, id, clientID, workspaceID, kind, authorType, authorID, authorName, body)
	return scanMessage(row)
}

// messageByClientID implements idempotent submission (§9): a retried send
// with the same client_id returns the message that already exists rather than
// creating a duplicate.
func (r *repository) messageByClientID(ctx context.Context, tx pgx.Tx, conversationID, clientID string) (*Message, error) {
	row := tx.QueryRow(ctx, `SELECT `+messageColumns+`
		FROM messages WHERE conversation_id = $1 AND client_id = $2
	`, conversationID, clientID)
	return scanMessage(row)
}

// touchConversation updates the denormalised list-view columns after a
// message is appended, and — for a customer message — last_customer_at, which
// SLA first-response calculations key off. Locks the row first (SELECT …
// FOR UPDATE) so the sequence allocation above and this update observe a
// consistent view under concurrent sends.
func (r *repository) touchConversation(
	ctx context.Context, tx pgx.Tx, conversationID string, preview string, isCustomer bool, at time.Time,
) error {
	if isCustomer {
		_, err := tx.Exec(ctx, `
			UPDATE conversations
			SET message_count = message_count + 1,
			    last_message_preview = $2,
			    last_message_at = $3,
			    last_customer_at = $3,
			    state = CASE WHEN state = 'new' THEN 'open' ELSE state END
			WHERE id = $1
		`, conversationID, preview, at)
		return err
	}

	_, err := tx.Exec(ctx, `
		UPDATE conversations
		SET message_count = message_count + 1,
		    last_message_preview = $2,
		    last_message_at = $3
		WHERE id = $1
	`, conversationID, preview, at)
	return err
}

// memberDisplayName resolves a member id to the user's current name, for
// denormalising into an audit entry at write time (mirrors
// internal/workspace's identical query — see that package for why this is
// duplicated rather than shared).
func (r *repository) memberDisplayName(ctx context.Context, tx pgx.Tx, memberID string) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `
		SELECT u.name FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.id = $1
	`, memberID).Scan(&name)
	return name, err
}

func (r *repository) listMessages(ctx context.Context, workspaceID, conversationID string, afterSequence int64) ([]Message, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+messageColumns+`
		FROM messages
		WHERE workspace_id = $1 AND conversation_id = $2 AND sequence > $3
		ORDER BY sequence ASC
	`, workspaceID, conversationID, afterSequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}
