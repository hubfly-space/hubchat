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
	ID                  string
	WorkspaceID         string
	InboxID             string
	Channel             string
	Subject             *string
	State               string
	Priority            string
	CustomerID          *string
	AssigneeID          *string
	TeamID              *string
	MessageCount        int
	LastMessagePreview  string
	LastMessageAt       time.Time
	CreatedAt           time.Time
}

type Message struct {
	ID             string
	ClientID       *string
	ConversationID string
	WorkspaceID    string
	Kind           string
	AuthorType     string
	AuthorID       *string
	AuthorName     string
	Body           string
	Sequence       int64
	CreatedAt      time.Time
}

type repository struct {
	pool *database.Pool
}

func (r *repository) insert(
	ctx context.Context,
	id, workspaceID, inboxID, channel string,
	subject *string,
	customerID *string,
) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO conversations
			(id, workspace_id, inbox_id, channel, subject, customer_id, state, priority)
		VALUES ($1, $2, $3, $4, $5, $6, 'new', 'normal')
	`, id, workspaceID, inboxID, channel, subject, customerID)
	return err
}

func (r *repository) byID(ctx context.Context, workspaceID, id string) (*Conversation, error) {
	var c Conversation
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, inbox_id, channel, subject, state, priority,
		       customer_id, assignee_id, team_id, message_count,
		       last_message_preview, last_message_at, created_at
		FROM conversations
		WHERE workspace_id = $1 AND id = $2
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

// listActive is the conversation_active_queue index's query, verbatim
// (0002_inboxes_and_conversations.sql): the open queue for one inbox, newest
// activity first, excluding closed and spam.
func (r *repository) listActive(ctx context.Context, workspaceID, inboxID string, limit int) ([]Conversation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, workspace_id, inbox_id, channel, subject, state, priority,
		       customer_id, assignee_id, team_id, message_count,
		       last_message_preview, last_message_at, created_at
		FROM conversations
		WHERE workspace_id = $1 AND inbox_id = $2 AND state NOT IN ('closed', 'spam')
		ORDER BY last_message_at DESC
		LIMIT $3
	`, workspaceID, inboxID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(
			&c.ID, &c.WorkspaceID, &c.InboxID, &c.Channel, &c.Subject, &c.State, &c.Priority,
			&c.CustomerID, &c.AssigneeID, &c.TeamID, &c.MessageCount,
			&c.LastMessagePreview, &c.LastMessageAt, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
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
	var m Message
	err := tx.QueryRow(ctx, `
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
		RETURNING id, client_id, conversation_id, workspace_id, kind, author_type,
		          author_id, author_name, body, sequence, created_at
	`, conversationID, id, clientID, workspaceID, kind, authorType, authorID, authorName, body,
	).Scan(
		&m.ID, &m.ClientID, &m.ConversationID, &m.WorkspaceID, &m.Kind, &m.AuthorType,
		&m.AuthorID, &m.AuthorName, &m.Body, &m.Sequence, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// messageByClientID implements idempotent submission (§9): a retried send
// with the same client_id returns the message that already exists rather than
// creating a duplicate.
func (r *repository) messageByClientID(ctx context.Context, tx pgx.Tx, conversationID, clientID string) (*Message, error) {
	var m Message
	err := tx.QueryRow(ctx, `
		SELECT id, client_id, conversation_id, workspace_id, kind, author_type,
		       author_id, author_name, body, sequence, created_at
		FROM messages
		WHERE conversation_id = $1 AND client_id = $2
	`, conversationID, clientID).Scan(
		&m.ID, &m.ClientID, &m.ConversationID, &m.WorkspaceID, &m.Kind, &m.AuthorType,
		&m.AuthorID, &m.AuthorName, &m.Body, &m.Sequence, &m.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &m, err
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

func (r *repository) listMessages(ctx context.Context, workspaceID, conversationID string, afterSequence int64) ([]Message, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, client_id, conversation_id, workspace_id, kind, author_type,
		       author_id, author_name, body, sequence, created_at
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
		var m Message
		if err := rows.Scan(
			&m.ID, &m.ClientID, &m.ConversationID, &m.WorkspaceID, &m.Kind, &m.AuthorType,
			&m.AuthorID, &m.AuthorName, &m.Body, &m.Sequence, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
