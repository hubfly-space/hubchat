// Package events owns the append-only workspace event log.
//
// # Responsibilities
//
// Allocates the per-workspace sequence, appends events inside the caller's
// transaction, replays ranges for resume, and signals other processes that new
// events exist.
//
// # Boundary
//
// Every state change worth telling anyone about goes through Append. Five
// subsystems read back out — realtime resume (§9), webhook delivery (§6.16),
// automation triggers (§6.13), notifications (§6.15), and analytics (§6.18) —
// and none of them invent their own record of what happened.
//
// Modules call Append; nothing outside this package writes to
// workspace_events, and nothing reads it except through Since or Range.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

// Type is a dotted event name. The set is closed: §16 forbids changing what an
// existing type means, so a new meaning gets a new constant rather than a
// quietly widened old one.
type Type string

const (
	ConversationCreated  Type = "conversation.created"
	ConversationUpdated  Type = "conversation.updated"
	ConversationAssigned Type = "conversation.assigned"
	ConversationStateSet Type = "conversation.state_changed"
	ConversationResolved Type = "conversation.resolved"
	ConversationMerged   Type = "conversation.merged"
	ConversationLinked   Type = "conversation.linked"
	ConversationUnlinked Type = "conversation.unlinked"

	MessageCreated  Type = "message.created"
	MessageEdited   Type = "message.edited"
	MessageRedacted Type = "message.redacted"
	MessageRead     Type = "message.read"

	TicketCreated     Type = "ticket.created"
	TicketUpdated     Type = "ticket.updated"
	TicketStateSet    Type = "ticket.state_changed"
	TicketSLABreached Type = "ticket.sla_breached"

	CustomerCreated    Type = "customer.created"
	CustomerUpdated    Type = "customer.updated"
	CustomerIdentified Type = "customer.identified"
	CustomerMerged     Type = "customer.merged"

	EventReceived     Type = "event.received"
	FormSubmitted     Type = "form.submitted"
	MemberJoined      Type = "member.joined"
	MemberProvisioned Type = "member.provisioned"
	MemberDeactivated Type = "member.deactivated"
	MemberReactivated Type = "member.reactivated"
	MemberRoleSet     Type = "member.role_changed"
	MemberRemoved     Type = "member.removed"
	RoleCreated       Type = "role.created"
	RoleUpdated       Type = "role.updated"
	RoleDeleted       Type = "role.deleted"
	InviteIssued      Type = "invite.issued"
	InviteAccepted    Type = "invite.accepted"
	InviteRevoked     Type = "invite.revoked"
	TeamCreated       Type = "team.created"
	TeamUpdated       Type = "team.updated"
	TeamDeleted       Type = "team.deleted"
	SavedViewCreated  Type = "saved_view.created"
	SavedViewUpdated  Type = "saved_view.updated"
	SavedViewDeleted  Type = "saved_view.deleted"
	TypingStarted     Type = "typing.started"
	PresenceUpdate    Type = "presence.updated"

	FeedbackCreated         Type = "feedback.created"
	FeedbackVoteRecorded    Type = "feedback.vote_recorded"
	FeedbackStatusChanged   Type = "feedback.status_changed"
	FeedbackLinked          Type = "feedback.linked"
	FeedbackMerged          Type = "feedback.merged"
	ChangelogPublished      Type = "changelog.published"
	ArticlePublished        Type = "article.published"
	ArticleViewed           Type = "article.viewed"
	ArticleFeedbackRecorded Type = "article.feedback_recorded"
	ArticleSearchRecorded   Type = "article.search_recorded"
	SurveyResponseCreated   Type = "survey.response_created"

	SLAApproaching Type = "sla.approaching"
	SLABreached    Type = "sla.breached"
)

// ActorType records who caused an event. It is not free-form: audit review and
// webhook consumers both branch on it.
type ActorType string

const (
	ActorUser       ActorType = "user"
	ActorCustomer   ActorType = "customer"
	ActorVisitor    ActorType = "visitor"
	ActorSystem     ActorType = "system"
	ActorAutomation ActorType = "automation"
	ActorAPIKey     ActorType = "api_key"
)

// Event is one thing that happened, before it has a sequence.
//
// Data is marshalled as-is into the envelope delivered to webhooks and
// realtime clients, so it must contain only what those consumers may see —
// this is a publication boundary, not an internal struct (§12: sensitive
// fields do not leave the service layer).
type Event struct {
	WorkspaceID string
	Type        Type
	EntityType  string
	EntityID    string
	ActorType   ActorType
	ActorID     string
	Data        any

	// CausationID links this event to the one that caused it, when an
	// automation produced it. The rule engine walks the chain to enforce its
	// depth cap (§26.7).
	CausationID string
	RequestID   string
}

// Record is a stored event, as returned by replay.
//
// The JSON tags are the wire envelope §16 specifies. This struct is what a
// WebSocket client and a webhook consumer both receive, which is why the field
// names are snake_case here rather than at some later mapping layer — one
// shape, defined once.
type Record struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Sequence    int64           `json:"sequence"`
	Type        Type            `json:"type"`
	EntityType  string          `json:"entity_type,omitempty"`
	EntityID    string          `json:"entity_id,omitempty"`
	ActorType   ActorType       `json:"-"`
	ActorID     string          `json:"-"`
	CausationID string          `json:"causation_id,omitempty"`
	OccurredAt  time.Time       `json:"occurred_at"`
	Data        json.RawMessage `json:"data"`
}

// ErrNotFound is returned when a specific event id does not resolve.
var ErrNotFound = errors.New("events: not found")

// Log is the append-only event log. Safe for concurrent use.
type Log struct {
	pool *database.Pool
}

// New returns a Log backed by pool.
func New(pool *database.Pool) *Log {
	return &Log{pool: pool}
}

// notifyChannel is the LISTEN/NOTIFY channel other processes subscribe to.
//
// The payload carries only the workspace id and the new sequence. NOTIFY
// payloads are capped at 8000 bytes, and an event's data can exceed that — so
// the signal says "there is something new for you, up to sequence N" and the
// listener reads the rows itself. That also means a listener that misses a
// notification recovers on the next one rather than losing an event.
const notifyChannel = "hubchat_events"

// Append writes one event inside the caller's transaction and returns it with
// its allocated sequence.
//
// Taking a transaction rather than opening one is the whole point: an event
// must commit atomically with the state change it describes. A message that
// exists without its `message.created` event would never reach a connected
// client, and an event without its message would deliver a webhook for
// something that does not exist.
func (l *Log) Append(ctx context.Context, tx pgx.Tx, event Event) (*Record, error) {
	if event.WorkspaceID == "" {
		return nil, errors.New("events: workspace id is required")
	}
	if event.Type == "" {
		return nil, errors.New("events: type is required")
	}
	if event.ActorType == "" {
		event.ActorType = ActorSystem
	}

	payload, err := json.Marshal(orEmptyObject(event.Data))
	if err != nil {
		return nil, fmt.Errorf("events: marshal data: %w", err)
	}

	sequence, err := nextSequence(ctx, tx, event.WorkspaceID)
	if err != nil {
		return nil, err
	}

	record := &Record{
		ID:          ids.New(ids.PrefixEvent),
		WorkspaceID: event.WorkspaceID,
		Sequence:    sequence,
		Type:        event.Type,
		EntityType:  event.EntityType,
		EntityID:    event.EntityID,
		ActorType:   event.ActorType,
		ActorID:     event.ActorID,
		CausationID: event.CausationID,
		Data:        payload,
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO workspace_events (
			id, workspace_id, sequence, type, entity_type, entity_id,
			actor_type, actor_id, data, causation_id, request_id
		)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''),
		        $7, NULLIF($8, ''), $9, NULLIF($10, ''), NULLIF($11, ''))
		RETURNING occurred_at
	`,
		record.ID, record.WorkspaceID, record.Sequence, string(record.Type),
		record.EntityType, record.EntityID, string(record.ActorType), record.ActorID,
		payload, event.CausationID, event.RequestID,
	).Scan(&record.OccurredAt)
	if err != nil {
		return nil, fmt.Errorf("events: insert: %w", err)
	}

	// Queued by PostgreSQL and delivered only if this transaction commits, so
	// a rolled-back append never wakes anyone up.
	if _, err := tx.Exec(ctx,
		`SELECT pg_notify($1, $2)`,
		notifyChannel,
		fmt.Sprintf("%s:%d", record.WorkspaceID, record.Sequence),
	); err != nil {
		return nil, fmt.Errorf("events: notify: %w", err)
	}

	return record, nil
}

// nextSequence allocates the next sequence for a workspace.
//
// The INSERT ... ON CONFLICT DO UPDATE is an upsert that doubles as the lock:
// the first event for a workspace creates the counter row, every later one
// takes a row lock on it. That lock is held until the caller's transaction
// commits, which is what makes sequence order and commit order agree — see the
// comment on workspace_event_sequences in migration 0004 for why that matters
// more than the throughput it costs.
func nextSequence(ctx context.Context, tx pgx.Tx, workspaceID string) (int64, error) {
	var sequence int64
	err := tx.QueryRow(ctx, `
		INSERT INTO workspace_event_sequences (workspace_id, next_sequence)
		VALUES ($1, 2)
		ON CONFLICT (workspace_id) DO UPDATE
			SET next_sequence = workspace_event_sequences.next_sequence + 1
		RETURNING next_sequence - 1
	`, workspaceID).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("events: allocate sequence: %w", err)
	}
	return sequence, nil
}

// Since returns up to limit events after the given sequence, oldest first.
//
// This is the resume query: a client that reconnects holding sequence N asks
// for everything after it. Ordering is by sequence, never by occurred_at —
// two events can share a microsecond, and the sequence is the only total
// order.
func (l *Log) Since(ctx context.Context, workspaceID string, afterSequence int64, limit int) ([]Record, error) {
	if limit <= 0 || limit > maxReplayLimit {
		limit = maxReplayLimit
	}

	rows, err := l.pool.Query(ctx, `
		SELECT id, workspace_id, sequence, type,
		       coalesce(entity_type, ''), coalesce(entity_id, ''),
		       actor_type, coalesce(actor_id, ''), data, coalesce(causation_id, ''), occurred_at
		FROM workspace_events
		WHERE workspace_id = $1 AND sequence > $2
		ORDER BY sequence
		LIMIT $3
	`, workspaceID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("events: replay: %w", err)
	}
	defer rows.Close()

	return scanRecords(rows)
}

// maxReplayLimit caps one replay page.
//
// A client that has been away for a week must not be able to ask the server to
// materialise a week of events into one frame. It gets a page, applies it,
// and asks again from its new position — which is also how a slow client is
// kept from holding a connection's worth of memory (§9 bounded outbound
// queues).
const maxReplayLimit = 500

// LatestSequence returns the highest sequence assigned in a workspace, or 0 if
// nothing has happened yet. New clients start here rather than at 0, so
// connecting does not replay the workspace's entire history.
func (l *Log) LatestSequence(ctx context.Context, workspaceID string) (int64, error) {
	var sequence int64
	err := l.pool.QueryRow(ctx, `
		SELECT coalesce(max(sequence), 0)
		FROM workspace_events
		WHERE workspace_id = $1
	`, workspaceID).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("events: latest sequence: %w", err)
	}
	return sequence, nil
}

// ForEntity returns the most recent events about one entity, newest first.
// Backs the activity timeline on a conversation, ticket, or customer.
func (l *Log) ForEntity(ctx context.Context, workspaceID, entityType, entityID string, limit int) ([]Record, error) {
	if limit <= 0 || limit > maxReplayLimit {
		limit = maxReplayLimit
	}

	rows, err := l.pool.Query(ctx, `
		SELECT id, workspace_id, sequence, type,
		       coalesce(entity_type, ''), coalesce(entity_id, ''),
		       actor_type, coalesce(actor_id, ''), data, coalesce(causation_id, ''), occurred_at
		FROM workspace_events
		WHERE workspace_id = $1 AND entity_type = $2 AND entity_id = $3
		ORDER BY sequence DESC
		LIMIT $4
	`, workspaceID, entityType, entityID, limit)
	if err != nil {
		return nil, fmt.Errorf("events: for entity: %w", err)
	}
	defer rows.Close()

	return scanRecords(rows)
}

// Get resolves one event by id, scoped to a workspace.
//
// The workspace predicate is not redundant with the primary key: without it,
// an id guessed or leaked from another tenant would resolve (§11.3, and §11.6
// makes a missing predicate a critical defect).
func (l *Log) Get(ctx context.Context, workspaceID, id string) (*Record, error) {
	rows, err := l.pool.Query(ctx, `
		SELECT id, workspace_id, sequence, type,
		       coalesce(entity_type, ''), coalesce(entity_id, ''),
		       actor_type, coalesce(actor_id, ''), data, coalesce(causation_id, ''), occurred_at
		FROM workspace_events
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id)
	if err != nil {
		return nil, fmt.Errorf("events: get: %w", err)
	}
	defer rows.Close()

	records, err := scanRecords(rows)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	return &records[0], nil
}

func scanRecords(rows pgx.Rows) ([]Record, error) {
	var records []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(
			&r.ID, &r.WorkspaceID, &r.Sequence, &r.Type,
			&r.EntityType, &r.EntityID, &r.ActorType, &r.ActorID,
			&r.Data, &r.CausationID, &r.OccurredAt,
		); err != nil {
			return nil, fmt.Errorf("events: scan: %w", err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("events: iterate: %w", err)
	}
	return records, nil
}

// orEmptyObject keeps `data` a JSON object rather than null when a caller has
// nothing to attach. Consumers can then always index into it, instead of every
// one of them null-checking first.
func orEmptyObject(data any) any {
	if data == nil {
		return struct{}{}
	}
	return data
}
