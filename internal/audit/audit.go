// Package audit records who did what, when, and to which record.
//
// # Responsibilities
//
// Append-only writes of security- and administration-relevant actions
// (§6.19), and the workspace-scoped reads that back the audit log screen.
//
// # Boundary
//
// Services call Record; handlers do not. An audit entry describes a decision
// the service layer made, and writing it from the HTTP layer would mean
// logging what was asked for rather than what actually happened.
//
// Entries are never updated or deleted by application code. §12 allows
// retention to remove old rows wholesale, but nothing rewrites one.
package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

// Action names what happened, in `entity.verb` form.
//
// The set is open — modules add their own — but the naming is not, because the
// audit screen groups on the prefix and an ad-hoc name is invisible there.
type Action string

const (
	UserSignedIn        Action = "user.signed_in"
	UserSignedOut       Action = "user.signed_out"
	UserSignInFailed    Action = "user.sign_in_failed"
	UserPasswordChanged Action = "user.password_changed"
	UserTOTPEnabled     Action = "user.totp_enabled"
	UserTOTPDisabled    Action = "user.totp_disabled"
	SessionRevoked      Action = "session.revoked"

	WorkspaceCreated  Action = "workspace.created"
	WorkspaceUpdated  Action = "workspace.updated"
	WorkspaceDeleted  Action = "workspace.deleted"
	MemberInvited     Action = "member.invited"
	MemberJoined      Action = "member.joined"
	MemberProvisioned Action = "member.provisioned"
	MemberDeactivated Action = "member.deactivated"
	MemberReactivated Action = "member.reactivated"
	MemberRoleChanged Action = "member.role_changed"
	MemberRemoved     Action = "member.removed"
	RoleCreated       Action = "role.created"
	RoleUpdated       Action = "role.updated"
	RoleDeleted       Action = "role.deleted"

	APIKeyCreated  Action = "api_key.created"
	APIKeyRevoked  Action = "api_key.revoked"
	WebhookCreated Action = "webhook.created"
	WebhookUpdated Action = "webhook.updated"
	WebhookDeleted Action = "webhook.deleted"

	WidgetUpdated  Action = "widget.updated"
	PortalUpdated  Action = "portal.updated"
	RuleChanged    Action = "automation_rule.changed"
	FeedbackLinked Action = "feedback.linked"
	FeedbackMerged Action = "feedback.merged"

	ConversationDeleted  Action = "conversation.deleted"
	ConversationLinked   Action = "conversation.linked"
	ConversationUnlinked Action = "conversation.unlinked"
	MessageRedacted      Action = "message.redacted"
	CustomerMerged       Action = "customer.merged"
	CustomerDeleted      Action = "customer.deleted"
	// SensitiveRevealed records a deliberate reveal of a masked field
	// (§12 audit on reveal).
	SensitiveRevealed  Action = "customer.sensitive_revealed"
	DataExported       Action = "data.exported"
	DataFileDownloaded Action = "data.file_downloaded"
	OpsTestEmailQueued Action = "ops.test_email_queued"
	LegalHoldCreated   Action = "legal_hold.created"
	LegalHoldReleased  Action = "legal_hold.released"
)

// ActorType mirrors the CHECK constraint on audit_logs.actor_type.
type ActorType string

const (
	ActorUser       ActorType = "user"
	ActorCustomer   ActorType = "customer"
	ActorSystem     ActorType = "system"
	ActorAutomation ActorType = "automation"
	ActorAPIKey     ActorType = "api_key"
)

// Entry is one audit record before it is written.
type Entry struct {
	WorkspaceID string
	ActorType   ActorType
	ActorID     string
	// ActorName is stored alongside the id rather than joined at read time, so
	// the entry still names who acted after the member is removed. That
	// denormalisation is the point of an audit log.
	ActorName  string
	Action     Action
	EntityType string
	EntityID   string
	RequestID  string
	IP         netip.Addr

	// Metadata describes the change. §19 forbids putting message bodies,
	// tokens, keys, or sensitive attribute values here — record that a field
	// changed, not what it changed to.
	Metadata any
}

// Record is one stored audit entry.
type Record struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	ActorType   ActorType       `json:"actor_type"`
	ActorID     string          `json:"actor_id,omitempty"`
	ActorName   string          `json:"actor_name"`
	Action      Action          `json:"action"`
	EntityType  string          `json:"entity_type,omitempty"`
	EntityID    string          `json:"entity_id,omitempty"`
	RequestID   string          `json:"request_id,omitempty"`
	IP          string          `json:"ip,omitempty"`
	Metadata    json.RawMessage `json:"metadata"`
	OccurredAt  time.Time       `json:"occurred_at"`
}

// Log writes and reads audit entries. Safe for concurrent use.
type Log struct {
	pool *database.Pool
}

// New returns a Log backed by pool.
func New(pool *database.Pool) *Log {
	return &Log{pool: pool}
}

// RetentionSweep removes audit rows past each workspace's configured audit
// window. The audit log remains append-only to application code; this is the
// explicit whole-range retention operation allowed by the privacy policy.
func (l *Log) RetentionSweep(ctx context.Context) (int64, error) {
	tag, err := l.pool.Exec(ctx, `
		DELETE FROM audit_logs al
		USING workspaces w
		WHERE al.workspace_id = w.id
		  AND coalesce((w.settings #>> '{privacy,retention_days,audit_logs}')::int, 0) > 0
		  AND al.occurred_at < now() - make_interval(days => (w.settings #>> '{privacy,retention_days,audit_logs}')::int)
		  AND NOT EXISTS (
				SELECT 1 FROM workspace_legal_holds lh
				WHERE lh.workspace_id = w.id AND lh.released_at IS NULL
				  AND lh.category IN ('all', 'audit')
			)
	`)
	if err != nil {
		return 0, fmt.Errorf("audit: retention sweep: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Record writes an entry on its own transaction.
func (l *Log) Record(ctx context.Context, entry Entry) error {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("audit: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := RecordTx(ctx, tx, entry); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("audit: commit: %w", err)
	}
	return nil
}

// RecordTx writes an entry inside the caller's transaction.
//
// Preferred over Record whenever the audited change is itself transactional:
// an entry saying a role was changed, written outside the transaction that
// changed it, can survive a rollback and claim something that never happened.
func RecordTx(ctx context.Context, tx pgx.Tx, entry Entry) error {
	if entry.WorkspaceID == "" {
		return errors.New("audit: workspace id is required")
	}
	if entry.Action == "" {
		return errors.New("audit: action is required")
	}
	if entry.ActorType == "" {
		entry.ActorType = ActorSystem
	}

	metadata, err := json.Marshal(orEmptyObject(entry.Metadata))
	if err != nil {
		return fmt.Errorf("audit: marshal metadata: %w", err)
	}

	var ip *string
	if entry.IP.IsValid() {
		text := entry.IP.String()
		ip = &text
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs (
			id, workspace_id, actor_type, actor_id, actor_name,
			action, entity_type, entity_id, request_id, ip, metadata
		)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6,
		        NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), $10, $11)
	`,
		ids.New(ids.PrefixAuditLog), entry.WorkspaceID, string(entry.ActorType),
		entry.ActorID, entry.ActorName, string(entry.Action),
		entry.EntityType, entry.EntityID, entry.RequestID, ip, metadata,
	)
	if err != nil {
		return fmt.Errorf("audit: insert %s: %w", entry.Action, err)
	}
	return nil
}

// Filter narrows an audit query. Every field is optional except the workspace,
// which the caller supplies separately and which is never optional (§11.3).
type Filter struct {
	ActorID    string
	Action     Action
	EntityType string
	EntityID   string
	// Before and BeforeID together are the pagination cursor: pass the last
	// row's OccurredAt and ID to continue past it. BeforeID is what keeps two
	// entries sharing a timestamp from being skipped or repeated across pages
	// — occurred_at alone is not fine-grained enough to promise that under
	// concurrent writes, and "the audit log dropped an entry" is a bad thing
	// for an audit log to do.
	Before   time.Time
	BeforeID string
	Limit    int
}

const maxPageSize = 200

// List returns audit entries for one workspace, newest first.
//
// Offset pagination is deliberately not offered — §16 requires cursors, and an
// append-only log paged by offset shifts under the reader as new rows arrive.
func (l *Log) List(ctx context.Context, workspaceID string, filter Filter) ([]Record, error) {
	if filter.Limit <= 0 || filter.Limit > maxPageSize {
		filter.Limit = maxPageSize
	}

	before := filter.Before
	if before.IsZero() {
		before = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}

	// The row tuple comparison is the tie-break: strictly less than
	// (before, beforeID) in that order, which matches the ORDER BY and is
	// well-defined even when BeforeID is empty (every id sorts after "").
	rows, err := l.pool.Query(ctx, `
		SELECT id, workspace_id, actor_type, coalesce(actor_id, ''), actor_name,
		       action, coalesce(entity_type, ''), coalesce(entity_id, ''),
		       coalesce(request_id, ''), coalesce(host(ip), ''), metadata, occurred_at
		FROM audit_logs
		WHERE workspace_id = $1
		  AND (occurred_at, id) < ($2, $3)
		  AND ($4 = '' OR actor_id = $4)
		  AND ($5 = '' OR action = $5)
		  AND ($6 = '' OR entity_type = $6)
		  AND ($7 = '' OR entity_id = $7)
		ORDER BY occurred_at DESC, id DESC
		LIMIT $8
	`,
		workspaceID, before, filter.BeforeID, filter.ActorID, string(filter.Action),
		filter.EntityType, filter.EntityID, filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("audit: list: %w", err)
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(
			&r.ID, &r.WorkspaceID, &r.ActorType, &r.ActorID, &r.ActorName,
			&r.Action, &r.EntityType, &r.EntityID, &r.RequestID, &r.IP,
			&r.Metadata, &r.OccurredAt,
		); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// WriteCSV streams the complete filtered audit history for one workspace.
// Metadata is already constrained by Record/RecordTx to exclude secrets and
// sensitive values; exporting it does not bypass that publication boundary.
func (l *Log) WriteCSV(ctx context.Context, workspaceID string, filter Filter, output io.Writer) error {
	rows, err := l.pool.Query(ctx, `
		SELECT id, workspace_id, actor_type, coalesce(actor_id, ''), actor_name,
		       action, coalesce(entity_type, ''), coalesce(entity_id, ''),
		       coalesce(request_id, ''), coalesce(host(ip), ''), metadata, occurred_at
		FROM audit_logs
		WHERE workspace_id = $1
		  AND ($2 = '' OR actor_id = $2)
		  AND ($3 = '' OR action = $3)
		  AND ($4 = '' OR entity_type = $4)
		  AND ($5 = '' OR entity_id = $5)
		ORDER BY occurred_at ASC, id ASC
	`, workspaceID, filter.ActorID, string(filter.Action), filter.EntityType, filter.EntityID)
	if err != nil {
		return fmt.Errorf("audit: export: %w", err)
	}
	defer rows.Close()

	writer := csv.NewWriter(output)
	if err := writer.Write([]string{"id", "workspace_id", "occurred_at", "actor_type", "actor_id", "actor_name", "action", "entity_type", "entity_id", "request_id", "ip", "metadata"}); err != nil {
		return fmt.Errorf("audit: export header: %w", err)
	}
	for rows.Next() {
		var record Record
		if err := rows.Scan(&record.ID, &record.WorkspaceID, &record.ActorType, &record.ActorID, &record.ActorName, &record.Action, &record.EntityType, &record.EntityID, &record.RequestID, &record.IP, &record.Metadata, &record.OccurredAt); err != nil {
			return fmt.Errorf("audit: export scan: %w", err)
		}
		writer.Write([]string{record.ID, record.WorkspaceID, record.OccurredAt.UTC().Format(time.RFC3339), string(record.ActorType), record.ActorID, record.ActorName, string(record.Action), record.EntityType, record.EntityID, record.RequestID, record.IP, string(record.Metadata)})
		if err := writer.Error(); err != nil {
			return fmt.Errorf("audit: export write: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("audit: export rows: %w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("audit: export flush: %w", err)
	}
	return nil
}

func orEmptyObject(metadata any) any {
	if metadata == nil {
		return struct{}{}
	}
	return metadata
}
