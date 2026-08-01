// Package emailchannel owns workspace mailboxes and inbound email threading.
// Outbound delivery is queued by the worker; this package records the durable
// message contract and resolves replies to conversations without relying on
// subject-line matching.
package emailchannel

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound          = errors.New("emailchannel: mailbox not found")
	ErrInvalidAddress    = errors.New("emailchannel: mailbox address is invalid")
	ErrInvalidMode       = errors.New("emailchannel: inbound mode is invalid")
	ErrInvalidInbox      = errors.New("emailchannel: inbox is not in this workspace")
	ErrInvalidMessage    = errors.New("emailchannel: inbound message is invalid")
	ErrSenderBlocked     = errors.New("emailchannel: sender is blocked")
	ErrSignature         = errors.New("emailchannel: inbound signature is invalid")
	ErrDuplicateMessage  = errors.New("emailchannel: message was already received")
	ErrSecretUnavailable = errors.New("emailchannel: mailbox secret is unavailable")
	ErrInvalidDelivery   = errors.New("emailchannel: delivery event is invalid")
)

type Service struct {
	pool         *database.Pool
	conversation *conversation.Service
	customer     *customer.Service
	inbox        *inbox.Service
	key          []byte
	jobs         *jobs.Client
	files        *filemodule.Service
	seenMu       sync.Mutex
	seen         map[string]int64
}

type Mailbox struct {
	ID                      string     `json:"id"`
	WorkspaceID             string     `json:"workspace_id"`
	InboxID                 string     `json:"inbox_id"`
	Address                 string     `json:"address"`
	DisplayName             string     `json:"display_name"`
	InboundMode             string     `json:"inbound_mode"`
	IMAPHost                *string    `json:"imap_host,omitempty"`
	IMAPPort                *int       `json:"imap_port,omitempty"`
	IMAPUsername            *string    `json:"imap_username,omitempty"`
	IMAPConfigured          bool       `json:"imap_configured"`
	InboundSecretConfigured bool       `json:"inbound_secret_configured"`
	AllowedSenders          []string   `json:"allowed_senders"`
	BlockedSenders          []string   `json:"blocked_senders"`
	Enabled                 bool       `json:"enabled"`
	LastPolledAt            *time.Time `json:"last_polled_at,omitempty"`
	LastError               *string    `json:"last_error,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

type CreateInput struct {
	InboxID        string   `json:"inbox_id"`
	Address        string   `json:"address"`
	DisplayName    string   `json:"display_name"`
	InboundMode    string   `json:"inbound_mode"`
	IMAPHost       *string  `json:"imap_host"`
	IMAPPort       *int     `json:"imap_port"`
	IMAPUsername   *string  `json:"imap_username"`
	IMAPPassword   string   `json:"imap_password"`
	InboundSecret  string   `json:"inbound_secret"`
	AllowedSenders []string `json:"allowed_senders"`
	BlockedSenders []string `json:"blocked_senders"`
	Enabled        bool     `json:"enabled"`
}

type UpdateInput struct {
	InboxID        *string  `json:"inbox_id"`
	Address        *string  `json:"address"`
	DisplayName    *string  `json:"display_name"`
	InboundMode    *string  `json:"inbound_mode"`
	IMAPHost       *string  `json:"imap_host"`
	IMAPPort       *int     `json:"imap_port"`
	IMAPUsername   *string  `json:"imap_username"`
	IMAPPassword   string   `json:"imap_password"`
	InboundSecret  string   `json:"inbound_secret"`
	AllowedSenders []string `json:"allowed_senders"`
	BlockedSenders []string `json:"blocked_senders"`
	Enabled        *bool    `json:"enabled"`
}

type Created struct {
	Mailbox       Mailbox `json:"mailbox"`
	InboundSecret string  `json:"inbound_secret,omitempty"`
}

type InboundEmail struct {
	To          []string            `json:"to"`
	From        string              `json:"from"`
	Subject     string              `json:"subject"`
	Body        string              `json:"body"`
	MessageID   string              `json:"message_id"`
	InReplyTo   string              `json:"in_reply_to"`
	References  []string            `json:"references"`
	Attachments []InboundAttachment `json:"attachments,omitempty"`
	ReceivedAt  time.Time           `json:"received_at"`
}

type InboundAttachment struct {
	Name     string
	MIMEType string
	Body     []byte
}

type IngestResult struct {
	EmailMessageID string `json:"email_message_id"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Created        bool   `json:"created"`
}

// DeliveryEvent is the normalized provider-independent delivery contract.
// Providers may retry the same event, so ProviderEventID is retained as the
// idempotency key in the delivery-event table.
type DeliveryEvent struct {
	ProviderEventID string    `json:"provider_event_id"`
	Type            string    `json:"type"`
	Recipient       string    `json:"recipient"`
	MessageID       string    `json:"message_id"`
	BounceType      string    `json:"bounce_type,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	Hard            bool      `json:"hard"`
	OccurredAt      time.Time `json:"occurred_at"`
}

type DeliveryEventView struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	MailboxID       string          `json:"mailbox_id"`
	Provider        string          `json:"provider"`
	ProviderEventID string          `json:"provider_event_id"`
	Type            string          `json:"type"`
	EmailMessageID  *string         `json:"email_message_id,omitempty"`
	Recipient       *string         `json:"recipient,omitempty"`
	BounceType      *string         `json:"bounce_type,omitempty"`
	Reason          *string         `json:"reason,omitempty"`
	Hard            bool            `json:"hard"`
	Payload         json.RawMessage `json:"payload"`
	OccurredAt      time.Time       `json:"occurred_at"`
	CreatedAt       time.Time       `json:"created_at"`
}

type Suppression struct {
	WorkspaceID string    `json:"workspace_id"`
	Address     string    `json:"address"`
	Reason      string    `json:"reason"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// outboundPayload is intentionally small and stable: it is persisted in the
// jobs table and may be retried by a later binary version.
type outboundPayload struct {
	WorkspaceID    string   `json:"workspace_id"`
	EmailMessageID string   `json:"email_message_id"`
	To             string   `json:"to"`
	Subject        string   `json:"subject"`
	Body           string   `json:"body"`
	ReplyTo        string   `json:"reply_to,omitempty"`
	MessageID      string   `json:"message_id,omitempty"`
	InReplyTo      string   `json:"in_reply_to,omitempty"`
	AttachmentIDs  []string `json:"attachment_ids,omitempty"`
}

const (
	jobSend       = "email.send"
	maxEventBatch = 200
)

func New(pool *database.Pool, secretKey []byte, conversationService *conversation.Service, customerService *customer.Service, inboxService *inbox.Service, queue ...*jobs.Client) *Service {
	key := sha256.Sum256(append([]byte("hubchat/email-secrets:"), secretKey...))
	var jobClient *jobs.Client
	if len(queue) > 0 {
		jobClient = queue[0]
	}
	return &Service{pool: pool, key: key[:], conversation: conversationService, customer: customerService, inbox: inboxService, jobs: jobClient, seen: make(map[string]int64)}
}

func (s *Service) SetFileService(files *filemodule.Service) {
	s.files = files
}

// RunEventConsumer queues one durable outbound email for each agent reply in
// an email conversation. The event log is the source of truth; the unique
// RFC message id and job dedupe key make a replay harmless.
func (s *Service) RunEventConsumer(ctx context.Context, signals <-chan events.Signal, source interface {
	Since(context.Context, string, int64, int) ([]events.Record, error)
}) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-signals:
			if !ok {
				return
			}
			s.seenMu.Lock()
			after, exists := s.seen[signal.WorkspaceID]
			if !exists {
				after = signal.Sequence - 1
			}
			s.seenMu.Unlock()
			for {
				records, err := source.Since(ctx, signal.WorkspaceID, after, maxEventBatch)
				if err != nil || len(records) == 0 {
					break
				}
				failed := false
				for _, record := range records {
					if err := s.processEvent(ctx, record); err != nil {
						failed = true
						break
					}
					after = record.Sequence
				}
				if failed {
					break
				}
				s.seenMu.Lock()
				s.seen[signal.WorkspaceID] = after
				s.seenMu.Unlock()
				if len(records) < maxEventBatch {
					break
				}
			}
		}
	}
}

func (s *Service) processEvent(ctx context.Context, record events.Record) error {
	if record.Type != events.MessageCreated {
		return nil
	}
	var message conversation.MessageEvent
	if err := json.Unmarshal(record.Data, &message); err != nil {
		return fmt.Errorf("emailchannel: decode message event: %w", err)
	}
	if message.AuthorType != "agent" || message.Kind != "reply" || strings.TrimSpace(message.Body) == "" {
		return nil
	}
	return s.queueOutbound(ctx, record.WorkspaceID, message)
}

func (s *Service) queueOutbound(ctx context.Context, workspaceID string, message conversation.MessageEvent) error {
	if s.jobs == nil {
		return errors.New("emailchannel: outbound queue is unavailable")
	}
	var mailboxAddress, recipient, subject, inReplyTo string
	err := s.pool.QueryRow(ctx, `
		SELECT m.address::text,
		       coalesce(verified.email::text, CASE WHEN cst.verification='verified' THEN cst.email::text ELSE '' END),
		       coalesce(c.subject,''),
		       coalesce(previous.message_id_header,'')
		FROM conversations c
		JOIN customers cst ON cst.workspace_id=c.workspace_id AND cst.id=c.customer_id
		JOIN email_mailboxes m ON m.workspace_id=c.workspace_id AND m.inbox_id=c.inbox_id AND m.enabled
		LEFT JOIN LATERAL (
			SELECT ce.email FROM customer_emails ce
			WHERE ce.workspace_id=c.workspace_id AND ce.customer_id=c.customer_id AND ce.verified_at IS NOT NULL
			ORDER BY ce.is_primary DESC, ce.created_at DESC LIMIT 1
		) verified ON true
		LEFT JOIN LATERAL (
			SELECT em.message_id_header FROM email_messages em
			WHERE em.workspace_id=c.workspace_id AND em.conversation_id=c.id AND em.direction='inbound' AND em.message_id_header IS NOT NULL
			ORDER BY em.created_at DESC, em.id DESC LIMIT 1
		) previous ON true
		LEFT JOIN email_suppressions suppression
			ON suppression.workspace_id=c.workspace_id
			AND suppression.address::text=coalesce(verified.email::text, CASE WHEN cst.verification='verified' THEN cst.email::text ELSE '' END)
		WHERE c.workspace_id=$1 AND c.id=$2 AND c.channel='email'
		  AND suppression.address IS NULL
		ORDER BY m.created_at, m.id
		LIMIT 1`, workspaceID, message.ConversationID).Scan(&mailboxAddress, &recipient, &subject, &inReplyTo)
	if errors.Is(err, pgx.ErrNoRows) || strings.TrimSpace(recipient) == "" {
		// Anonymous or unverified conversations are valid, but there is no safe
		// address to mail. They remain HTTP/widget-only until identity is proven.
		return nil
	}
	if err != nil {
		return fmt.Errorf("emailchannel: resolve outbound recipient: %w", err)
	}

	header := outboundHeader(message.MessageID, mailboxAddress)
	if subject == "" {
		subject = "Your Hubchat support conversation"
	} else if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		subject = "Re: " + subject
	}
	var emailID, existingStatus string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO email_messages
		(id,workspace_id,mailbox_id,direction,message_id,conversation_id,message_id_header,in_reply_to,references_headers,from_address,to_addresses,subject,status)
		SELECT $2, $1, m.id, 'outbound', $3, $4, $5, NULLIF($6,''),
		       CASE WHEN NULLIF($6,'') IS NULL THEN '{}'::text[] ELSE ARRAY[$6]::text[] END,
		       m.address, ARRAY[$7]::text[], $8, 'pending'
		FROM email_mailboxes m
		WHERE m.workspace_id=$1 AND m.address=$9 AND m.enabled
		ON CONFLICT (workspace_id,message_id_header) DO NOTHING
		RETURNING id`, workspaceID, ids.New(ids.PrefixEmailMessage), message.MessageID, message.ConversationID, header, inReplyTo, recipient, subject, mailboxAddress).Scan(&emailID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `SELECT id,status FROM email_messages WHERE workspace_id=$1 AND message_id_header=$2`, workspaceID, header).Scan(&emailID, &existingStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("emailchannel: find outbound message: %w", err)
		}
		if existingStatus == "sent" || existingStatus == "delivered" {
			return nil
		}
	} else if err != nil {
		return fmt.Errorf("emailchannel: record outbound message: %w", err)
	}
	var attachmentIDs []string
	if s.files != nil {
		attachments, attachmentErr := s.files.MessageAttachments(ctx, workspaceID, message.MessageID)
		if attachmentErr != nil {
			return fmt.Errorf("emailchannel: load outbound attachments: %w", attachmentErr)
		}
		attachmentIDs = make([]string, 0, len(attachments))
		for _, attachment := range attachments {
			attachmentIDs = append(attachmentIDs, attachment.ID)
		}
	}
	_, err = s.jobs.Enqueue(ctx, jobs.Spec{
		WorkspaceID: workspaceID,
		Queue:       "email",
		Type:        jobSend,
		Payload: outboundPayload{WorkspaceID: workspaceID, EmailMessageID: emailID, To: recipient,
			Subject: subject, Body: message.Body, ReplyTo: mailboxAddress, MessageID: header, InReplyTo: inReplyTo, AttachmentIDs: attachmentIDs},
		DedupeKey: "email-outbound:" + emailID,
	})
	if errors.Is(err, jobs.ErrDuplicate) {
		return nil
	}
	if err != nil {
		_ = s.MarkFailed(ctx, workspaceID, emailID, err)
		return err
	}
	return nil
}

func outboundHeader(messageID, mailboxAddress string) string {
	parsed, err := mail.ParseAddress(mailboxAddress)
	if err != nil || parsed == nil || !strings.Contains(parsed.Address, "@") {
		return "<" + messageID + "@hubchat.invalid>"
	}
	return "<" + messageID + "@" + strings.SplitN(parsed.Address, "@", 2)[1] + ">"
}

// MarkSent and MarkFailed are called by the worker after the SMTP attempt.
// Both predicates include the workspace so a malformed job cannot update a
// same-named record in another tenant.
func (s *Service) MarkSent(ctx context.Context, workspaceID, emailID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE email_messages SET status='sent',sent_at=now(),error=NULL WHERE workspace_id=$1 AND id=$2 AND direction='outbound'`, workspaceID, emailID)
	return err
}

func (s *Service) MarkFailed(ctx context.Context, workspaceID, emailID string, cause error) error {
	if cause == nil {
		cause = errors.New("email delivery failed")
	}
	_, err := s.pool.Exec(ctx, `UPDATE email_messages SET status='failed',error=left($3,2000) WHERE workspace_id=$1 AND id=$2 AND direction='outbound'`, workspaceID, emailID, cause.Error())
	return err
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Mailbox, error) {
	rows, err := s.pool.Query(ctx, mailboxSelect+` WHERE workspace_id=$1 ORDER BY address, id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("emailchannel: list: %w", err)
	}
	defer rows.Close()
	items := make([]Mailbox, 0)
	for rows.Next() {
		item, err := scanMailbox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Service) ListPage(ctx context.Context, workspaceID, beforeAddress, beforeID string, limit int) ([]Mailbox, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := "workspace_id=$1"
	args := []any{workspaceID}
	if beforeAddress != "" {
		where += " AND (address::text,id) > ($2,$3)"
		args = append(args, beforeAddress, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, mailboxSelect+` WHERE `+where+` ORDER BY address, id LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("emailchannel: list page: %w", err)
	}
	defer rows.Close()
	items := make([]Mailbox, 0)
	for rows.Next() {
		item, err := scanMailbox(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Mailbox, error) {
	item, err := scanMailbox(s.pool.QueryRow(ctx, mailboxSelect+` WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("emailchannel: get: %w", err)
	}
	return item, nil
}

func (s *Service) Create(ctx context.Context, workspaceID string, input CreateInput) (*Created, error) {
	address, err := normalizeAddress(input.Address)
	if err != nil {
		return nil, err
	}
	mode, err := normalizeMode(input.InboundMode)
	if err != nil {
		return nil, err
	}
	if s.inbox == nil {
		return nil, ErrInvalidInbox
	}
	if _, err := s.inbox.Get(ctx, workspaceID, input.InboxID); err != nil {
		return nil, ErrInvalidInbox
	}
	secret := strings.TrimSpace(input.InboundSecret)
	if mode == "webhook" && secret == "" {
		secret, err = randomSecret()
		if err != nil {
			return nil, err
		}
	}
	password, err := s.encrypt(strings.TrimSpace(input.IMAPPassword))
	if err != nil {
		return nil, err
	}
	encryptedSecret, err := s.encrypt(secret)
	if err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixMailbox)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO email_mailboxes
		(id,workspace_id,inbox_id,address,display_name,inbound_mode,imap_host,imap_port,imap_username,imap_password,inbound_secret,allowed_senders,blocked_senders,enabled)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::bytea,$11::bytea,$12,$13,$14)
	`, id, workspaceID, input.InboxID, address, strings.TrimSpace(input.DisplayName), mode, input.IMAPHost, input.IMAPPort, input.IMAPUsername, password, encryptedSecret, normalizeAddresses(input.AllowedSenders), normalizeAddresses(input.BlockedSenders), input.Enabled)
	if err != nil {
		return nil, fmt.Errorf("emailchannel: create: %w", err)
	}
	item, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return &Created{Mailbox: *item, InboundSecret: secret}, nil
}

func (s *Service) Update(ctx context.Context, workspaceID, id string, input UpdateInput) (*Mailbox, error) {
	if input.Address != nil {
		address, err := normalizeAddress(*input.Address)
		if err != nil {
			return nil, err
		}
		input.Address = &address
	}
	if input.InboundMode != nil {
		mode, err := normalizeMode(*input.InboundMode)
		if err != nil {
			return nil, err
		}
		input.InboundMode = &mode
	}
	if input.InboxID != nil && s.inbox != nil {
		if _, err := s.inbox.Get(ctx, workspaceID, *input.InboxID); err != nil {
			return nil, ErrInvalidInbox
		}
	}
	password, err := s.encrypt(strings.TrimSpace(input.IMAPPassword))
	if err != nil {
		return nil, err
	}
	secret, err := s.encrypt(strings.TrimSpace(input.InboundSecret))
	if err != nil {
		return nil, err
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE email_mailboxes SET inbox_id=COALESCE($3,inbox_id),address=COALESCE($4,address),display_name=COALESCE($5,display_name),inbound_mode=COALESCE($6,inbound_mode),imap_host=COALESCE($7,imap_host),imap_port=COALESCE($8,imap_port),imap_username=COALESCE($9,imap_username),imap_password=CASE WHEN $10::bytea IS NULL THEN imap_password ELSE $10::bytea END,inbound_secret=CASE WHEN $11::bytea IS NULL THEN inbound_secret ELSE $11::bytea END,allowed_senders=COALESCE($12,allowed_senders),blocked_senders=COALESCE($13,blocked_senders),enabled=COALESCE($14,enabled),updated_at=now()
		WHERE workspace_id=$1 AND id=$2
	`, workspaceID, id, input.InboxID, input.Address, input.DisplayName, input.InboundMode, input.IMAPHost, input.IMAPPort, input.IMAPUsername, password, secret, nullableStrings(input.AllowedSenders), nullableStrings(input.BlockedSenders), input.Enabled)
	if err != nil {
		return nil, fmt.Errorf("emailchannel: update: %w", err)
	}
	if result.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, workspaceID, id)
}

func (s *Service) Delete(ctx context.Context, workspaceID, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM email_mailboxes WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("emailchannel: delete: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) Ingest(ctx context.Context, raw []byte, signature string, input InboundEmail) (*IngestResult, error) {
	if len(input.To) == 0 || strings.TrimSpace(input.From) == "" || strings.TrimSpace(input.Body) == "" {
		return nil, ErrInvalidMessage
	}
	mailbox, secret, err := s.mailboxByRecipient(ctx, input.To)
	if err != nil {
		return nil, err
	}
	if mailbox.InboundMode != "webhook" || !mailbox.Enabled {
		return nil, ErrInvalidMessage
	}
	if err := verifySignature(raw, signature, secret); err != nil {
		return nil, err
	}
	return s.ingestResolved(ctx, mailbox, input)
}

func (s *Service) ingestResolved(ctx context.Context, mailbox *Mailbox, input InboundEmail) (*IngestResult, error) {
	from, err := normalizeAddress(input.From)
	if err != nil {
		return nil, ErrInvalidMessage
	}
	if !senderAllowed(from, mailbox.AllowedSenders, mailbox.BlockedSenders) {
		return nil, ErrSenderBlocked
	}
	if input.ReceivedAt.IsZero() {
		input.ReceivedAt = time.Now().UTC()
	}
	if input.MessageID != "" {
		var existing IngestResult
		err := s.pool.QueryRow(ctx, `SELECT em.id,coalesce(em.conversation_id,''),coalesce(em.message_id,''),false FROM email_messages em WHERE em.workspace_id=$1 AND em.message_id_header=$2`, mailbox.WorkspaceID, strings.TrimSpace(input.MessageID)).Scan(&existing.EmailMessageID, &existing.ConversationID, &existing.MessageID, &existing.Created)
		if err == nil {
			return &existing, ErrDuplicateMessage
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if s.conversation == nil || s.customer == nil {
		return nil, errors.New("emailchannel: conversation dependencies are unavailable")
	}
	customerID, customerName, err := s.resolveCustomer(ctx, mailbox.WorkspaceID, from)
	if err != nil {
		return nil, err
	}
	body := stripQuotedText(strings.TrimSpace(input.Body))
	if body == "" {
		return nil, ErrInvalidMessage
	}
	emailID := ids.New(ids.PrefixEmailMessage)
	_, err = s.pool.Exec(ctx, `INSERT INTO email_messages(id,workspace_id,mailbox_id,direction,message_id_header,in_reply_to,references_headers,from_address,to_addresses,subject,status,received_at) VALUES($1,$2,$3,'inbound',NULLIF($4,''),NULLIF($5,''),$6,$7,$8,NULLIF($9,''),'pending',$10)`, emailID, mailbox.WorkspaceID, mailbox.ID, strings.TrimSpace(input.MessageID), strings.TrimSpace(input.InReplyTo), input.References, from, input.To, strings.TrimSpace(input.Subject), input.ReceivedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateMessage
		}
		return nil, err
	}
	markFailed := func(cause error) {
		_, _ = s.pool.Exec(ctx, `UPDATE email_messages SET status='failed',error=left($3,2000) WHERE workspace_id=$1 AND id=$2`, mailbox.WorkspaceID, emailID, cause.Error())
	}
	var conversationID, messageID string
	threadID, err := s.threadConversation(ctx, mailbox.WorkspaceID, input)
	if err != nil {
		markFailed(err)
		return nil, err
	}
	customerPtr := &customerID
	if threadID == "" {
		subject := strings.TrimSpace(input.Subject)
		var subjectPtr *string
		if subject != "" {
			subjectPtr = &subject
		}
		created, message, err := s.conversation.Start(ctx, mailbox.WorkspaceID, mailbox.InboxID, "email", subjectPtr, customerPtr, nil, customerName, body)
		if err != nil {
			markFailed(err)
			return nil, err
		}
		conversationID, messageID = created.ID, message.ID
	} else {
		message, err := s.conversation.PostMessage(ctx, mailbox.WorkspaceID, threadID, nil, "reply", "customer", customerPtr, customerName, body)
		if err != nil {
			markFailed(err)
			return nil, err
		}
		conversationID, messageID = threadID, message.ID
	}
	if s.files != nil && len(input.Attachments) > 0 {
		fileIDs := make([]string, 0, len(input.Attachments))
		for _, attachment := range input.Attachments {
			if strings.TrimSpace(attachment.Name) == "" || len(attachment.Body) == 0 {
				continue
			}
			stored, storeErr := s.files.Create(ctx, mailbox.WorkspaceID, filemodule.UploadInput{
				Name: attachment.Name, MIMEType: attachment.MIMEType, SizeBytes: int64(len(attachment.Body)),
				Body: bytes.NewReader(attachment.Body), OwnerType: "message", OwnerID: messageID,
				UploadedByType: "customer", UploadedByID: customerID,
			})
			if storeErr != nil {
				markFailed(storeErr)
				return nil, fmt.Errorf("emailchannel: store inbound attachment: %w", storeErr)
			}
			fileIDs = append(fileIDs, stored.ID)
		}
		if err := s.files.AttachToMessage(ctx, mailbox.WorkspaceID, messageID, fileIDs); err != nil {
			markFailed(err)
			return nil, fmt.Errorf("emailchannel: attach inbound files: %w", err)
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE email_messages SET message_id=$3,conversation_id=$4,status='received',error=NULL WHERE workspace_id=$1 AND id=$2`, mailbox.WorkspaceID, emailID, messageID, conversationID)
	if err != nil {
		markFailed(err)
		return nil, err
	}
	return &IngestResult{EmailMessageID: emailID, ConversationID: conversationID, MessageID: messageID, Created: threadID == ""}, nil
}

// IngestDelivery records a provider delivery callback and projects its result
// onto the matching outbound email. The mailbox id is part of the public
// callback URL, while the mailbox secret still authenticates the raw body.
// Retried callbacks are acknowledged after the unique provider event key is
// found, without repeating status or suppression side effects.
func (s *Service) IngestDelivery(ctx context.Context, mailboxID, provider string, raw []byte, signature string, event DeliveryEvent) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || strings.TrimSpace(event.ProviderEventID) == "" || event.Type == "" {
		return ErrInvalidDelivery
	}
	mailbox, secret, err := s.mailboxByID(ctx, mailboxID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !mailbox.Enabled {
		return ErrInvalidDelivery
	}
	if err := verifySignature(raw, signature, secret); err != nil {
		return err
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.Recipient != "" {
		event.Recipient, err = normalizeAddress(event.Recipient)
		if err != nil {
			return ErrInvalidDelivery
		}
	}

	payload := raw
	if !json.Valid(payload) {
		payload, _ = json.Marshal(event)
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO email_delivery_events
			(id,workspace_id,mailbox_id,provider,provider_event_id,event_type,recipient,bounce_type,reason,hard,payload,occurred_at)
		VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11::jsonb,$12)
		ON CONFLICT (workspace_id,mailbox_id,provider,provider_event_id) DO NOTHING
		RETURNING id`, ids.New(ids.PrefixEmailDelivery), mailbox.WorkspaceID, mailbox.ID, provider,
		event.ProviderEventID, event.Type, event.Recipient, event.BounceType, event.Reason, event.Hard, payload, event.OccurredAt).Scan(new(string))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("emailchannel: record delivery event: %w", err)
	}

	messageID, err := s.matchOutboundMessage(ctx, mailbox.WorkspaceID, event)
	if err != nil {
		return err
	}
	status := event.Type
	if status == "bounced" {
		_, err = s.pool.Exec(ctx, `UPDATE email_messages SET status='bounced',bounce_type=NULLIF($3,''),error=NULLIF($4,'') WHERE workspace_id=$1 AND id=$2 AND direction='outbound'`, mailbox.WorkspaceID, messageID, event.BounceType, event.Reason)
	} else if status == "delivered" {
		_, err = s.pool.Exec(ctx, `UPDATE email_messages SET status='delivered',error=NULL WHERE workspace_id=$1 AND id=$2 AND direction='outbound'`, mailbox.WorkspaceID, messageID)
	} else if status == "failed" {
		_, err = s.pool.Exec(ctx, `UPDATE email_messages SET status='failed',error=NULLIF($3,'') WHERE workspace_id=$1 AND id=$2 AND direction='outbound'`, mailbox.WorkspaceID, messageID, event.Reason)
	}
	if err != nil {
		return fmt.Errorf("emailchannel: update delivery status: %w", err)
	}
	if event.Hard && event.Recipient != "" {
		_, err = s.pool.Exec(ctx, `
			INSERT INTO email_suppressions(workspace_id,address,reason,source)
			VALUES($1,$2,$3,$4)
			ON CONFLICT (workspace_id,address) DO UPDATE SET reason=EXCLUDED.reason,source=EXCLUDED.source,updated_at=now()
		`, mailbox.WorkspaceID, event.Recipient, nonEmpty(event.Reason, event.BounceType), provider)
		if err != nil {
			return fmt.Errorf("emailchannel: suppress bounced recipient: %w", err)
		}
	}
	return nil
}

func (s *Service) matchOutboundMessage(ctx context.Context, workspaceID string, event DeliveryEvent) (string, error) {
	if strings.TrimSpace(event.MessageID) != "" {
		var id string
		err := s.pool.QueryRow(ctx, `
			SELECT id FROM email_messages
			WHERE workspace_id=$1 AND direction='outbound'
			  AND (id=$2 OR message_id_header=$2)
			ORDER BY created_at DESC LIMIT 1`, workspaceID, strings.TrimSpace(event.MessageID)).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	if strings.TrimSpace(event.Recipient) == "" {
		return "", nil
	}
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM email_messages
		WHERE workspace_id=$1 AND direction='outbound' AND $2=ANY(to_addresses)
		  AND status IN ('pending','sent','delivered')
		ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID, event.Recipient).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Service) ListDeliveryEvents(ctx context.Context, workspaceID, mailboxID string, before time.Time, beforeID string, limit int) ([]DeliveryEventView, error) {
	if limit <= 0 || limit > 201 {
		limit = 100
	}
	query := `
		SELECT id,workspace_id,mailbox_id,provider,provider_event_id,event_type,email_message_id,
		       recipient,bounce_type,reason,hard,payload,occurred_at,created_at
		FROM email_delivery_events
		WHERE workspace_id=$1 AND mailbox_id=$2`
	args := []any{workspaceID, mailboxID}
	if !before.IsZero() {
		query += ` AND (occurred_at,id)<($3,$4)`
		args = append(args, before, beforeID)
	}
	query += ` ORDER BY occurred_at DESC,id DESC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("emailchannel: list delivery events: %w", err)
	}
	defer rows.Close()
	result := make([]DeliveryEventView, 0)
	for rows.Next() {
		var item DeliveryEventView
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.MailboxID, &item.Provider, &item.ProviderEventID, &item.Type,
			&item.EmailMessageID, &item.Recipient, &item.BounceType, &item.Reason, &item.Hard, &item.Payload, &item.OccurredAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) ListSuppressions(ctx context.Context, workspaceID string, limit int) ([]Suppression, error) {
	return s.ListSuppressionsPage(ctx, workspaceID, time.Time{}, "", limit)
}

// ListSuppressionsPage returns a workspace-scoped page ordered by the latest
// suppression update. The address is the deterministic tie-breaker because
// updated_at alone is not unique.
func (s *Service) ListSuppressionsPage(ctx context.Context, workspaceID string, before time.Time, beforeAddress string, limit int) ([]Suppression, error) {
	if limit <= 0 || limit > 201 {
		limit = 200
	}
	query := `
		SELECT workspace_id,address::text,reason,source,created_at,updated_at
		FROM email_suppressions WHERE workspace_id=$1`
	args := []any{workspaceID}
	if !before.IsZero() {
		query += ` AND (updated_at < $2 OR (updated_at = $2 AND address > $3))`
		args = append(args, before, beforeAddress)
	}
	query += ` ORDER BY updated_at DESC,address ASC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("emailchannel: list suppressions: %w", err)
	}
	defer rows.Close()
	result := make([]Suppression, 0)
	for rows.Next() {
		var item Suppression
		if err := rows.Scan(&item.WorkspaceID, &item.Address, &item.Reason, &item.Source, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) RemoveSuppression(ctx context.Context, workspaceID, address string) error {
	address, err := normalizeAddress(address)
	if err != nil {
		return ErrInvalidAddress
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM email_suppressions WHERE workspace_id=$1 AND address=$2`, workspaceID, address)
	if err != nil {
		return fmt.Errorf("emailchannel: remove suppression: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func nonEmpty(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func (s *Service) threadConversation(ctx context.Context, workspaceID string, input InboundEmail) (string, error) {
	headerIDs := make([]string, 0, len(input.References)+1)
	if input.InReplyTo != "" {
		headerIDs = append(headerIDs, strings.TrimSpace(input.InReplyTo))
	}
	for _, value := range input.References {
		if strings.TrimSpace(value) != "" {
			headerIDs = append(headerIDs, strings.TrimSpace(value))
		}
	}
	if len(headerIDs) == 0 {
		return "", nil
	}
	var id string
	err := s.pool.QueryRow(ctx, `SELECT conversation_id FROM email_messages WHERE workspace_id=$1 AND (message_id_header=ANY($2::text[]) OR in_reply_to=ANY($2::text[])) AND conversation_id IS NOT NULL ORDER BY created_at DESC LIMIT 1`, workspaceID, headerIDs).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Service) resolveCustomer(ctx context.Context, workspaceID, address string) (string, string, error) {
	items, err := s.customer.Search(ctx, workspaceID, address, 20)
	if err != nil {
		return "", "", err
	}
	for _, item := range items {
		if item.Email != nil && strings.EqualFold(*item.Email, address) {
			return item.ID, deref(item.Name, address), nil
		}
	}
	parsed, _ := mail.ParseAddress(address)
	name := address
	if parsed != nil && strings.TrimSpace(parsed.Name) != "" {
		name = strings.TrimSpace(parsed.Name)
	}
	created, err := s.customer.Identify(ctx, workspaceID, nil, &name, &address, nil, false)
	if err != nil {
		return "", "", err
	}
	return created.ID, name, nil
}

func (s *Service) mailboxByRecipient(ctx context.Context, recipients []string) (*Mailbox, string, error) {
	for _, recipient := range recipients {
		address, err := normalizeAddress(recipient)
		if err != nil {
			continue
		}
		item, secret, err := s.mailboxByAddress(ctx, address)
		if err == nil {
			return item, secret, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, "", err
		}
	}
	return nil, "", ErrNotFound
}

func (s *Service) mailboxByAddress(ctx context.Context, address string) (*Mailbox, string, error) {
	var encrypted []byte
	item, err := scanMailboxWithSecret(s.pool.QueryRow(ctx, mailboxSelectWithSecret+` WHERE address=$1`, address), &encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if len(encrypted) == 0 {
		return nil, "", ErrSecretUnavailable
	}
	secret, err := s.decrypt(encrypted)
	return item, secret, err
}

func (s *Service) mailboxByID(ctx context.Context, id string) (*Mailbox, string, error) {
	var encrypted []byte
	item, err := scanMailboxWithSecret(s.pool.QueryRow(ctx, mailboxSelectWithSecret+` WHERE id=$1`, id), &encrypted)
	if err != nil {
		return nil, "", err
	}
	if len(encrypted) == 0 {
		return nil, "", ErrSecretUnavailable
	}
	secret, err := s.decrypt(encrypted)
	return item, secret, err
}

const mailboxSelect = `SELECT id,workspace_id,inbox_id,address::text,display_name,inbound_mode,imap_host,imap_port,imap_username,imap_password IS NOT NULL,inbound_secret IS NOT NULL,allowed_senders,blocked_senders,enabled,last_polled_at,last_error,created_at,updated_at FROM email_mailboxes`
const mailboxSelectWithSecret = `SELECT id,workspace_id,inbox_id,address::text,display_name,inbound_mode,imap_host,imap_port,imap_username,imap_password IS NOT NULL,inbound_secret IS NOT NULL,allowed_senders,blocked_senders,enabled,last_polled_at,last_error,created_at,updated_at,inbound_secret FROM email_mailboxes`

func scanMailbox(row interface{ Scan(...any) error }) (*Mailbox, error) {
	var item Mailbox
	err := row.Scan(&item.ID, &item.WorkspaceID, &item.InboxID, &item.Address, &item.DisplayName, &item.InboundMode, &item.IMAPHost, &item.IMAPPort, &item.IMAPUsername, &item.IMAPConfigured, &item.InboundSecretConfigured, &item.AllowedSenders, &item.BlockedSenders, &item.Enabled, &item.LastPolledAt, &item.LastError, &item.CreatedAt, &item.UpdatedAt)
	return &item, err
}

func scanMailboxWithSecret(row interface{ Scan(...any) error }, secret *[]byte) (*Mailbox, error) {
	var item Mailbox
	err := row.Scan(&item.ID, &item.WorkspaceID, &item.InboxID, &item.Address, &item.DisplayName, &item.InboundMode, &item.IMAPHost, &item.IMAPPort, &item.IMAPUsername, &item.IMAPConfigured, &item.InboundSecretConfigured, &item.AllowedSenders, &item.BlockedSenders, &item.Enabled, &item.LastPolledAt, &item.LastError, &item.CreatedAt, &item.UpdatedAt, secret)
	return &item, err
}

func normalizeAddress(value string) (string, error) {
	parsed, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || parsed.Address == "" || !strings.Contains(parsed.Address, "@") {
		return "", ErrInvalidAddress
	}
	return strings.ToLower(strings.TrimSpace(parsed.Address)), nil
}

func normalizeMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "off"
	}
	if value != "webhook" && value != "imap" && value != "off" {
		return "", ErrInvalidMode
	}
	return value, nil
}

func normalizeAddresses(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func nullableStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return normalizeAddresses(values)
}

func senderAllowed(sender string, allowed, blocked []string) bool {
	domain := sender[strings.LastIndex(sender, "@")+1:]
	for _, value := range blocked {
		value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "@")
		if value == sender || value == domain {
			return false
		}
	}
	if len(allowed) == 0 {
		return true
	}
	for _, value := range allowed {
		value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "@")
		if value == sender || value == domain {
			return true
		}
	}
	return false
}

func stripQuotedText(body string) string {
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(strings.ToLower(trimmed), "on ") && strings.HasSuffix(strings.ToLower(trimmed), " wrote:") || strings.HasPrefix(strings.ToLower(trimmed), "from:") {
			break
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func verifySignature(body []byte, header, secret string) error {
	if strings.TrimSpace(secret) == "" {
		return ErrSecretUnavailable
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return ErrSignature
	}
	var timestamp, provided string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			timestamp = value
		case "v1", "sha256":
			provided = value
		}
	}
	if provided == "" {
		provided = strings.TrimPrefix(header, "sha256=")
	}
	signed := body
	if timestamp != "" {
		unix, parseErr := parseUnix(timestamp)
		if parseErr != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > 5*time.Minute {
			return ErrSignature
		}
		signed = []byte(timestamp + "." + string(body))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(signed)
	expected := hex.EncodeToString(mac.Sum(nil))
	provided = strings.TrimSpace(provided)
	if len(provided) != len(expected) || !hmac.Equal([]byte(strings.ToLower(provided)), []byte(expected)) {
		return ErrSignature
	}
	return nil
}

func parseUnix(value string) (int64, error) {
	var out int64
	_, err := fmt.Sscan(value, &out)
	return out, err
}

func (s *Service) encrypt(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(value), nil), nil
}

func (s *Service) decrypt(value []byte) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(value) < gcm.NonceSize() {
		return "", ErrSecretUnavailable
	}
	plain, err := gcm.Open(nil, value[:gcm.NonceSize()], value[gcm.NonceSize():], nil)
	if err != nil {
		return "", ErrSecretUnavailable
	}
	return string(plain), nil
}

func randomSecret() (string, error) {
	value := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return "em_" + hex.EncodeToString(value), nil
}

func deref(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return *value
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// UnmarshalProviderPayload accepts the generic JSON shape used by older
// integrations. Provider-aware callers should use UnmarshalProviderPayloadFor
// so Postmark JSON and Mailgun/SendGrid form posts retain their headers.
func UnmarshalProviderPayload(data []byte) (InboundEmail, error) {
	return UnmarshalProviderPayloadFor("generic", "application/json", data)
}

// UnmarshalProviderPayloadFor normalizes the common inbound providers without
// making the rest of the email channel know their wire formats. It accepts the
// raw body so the caller can authenticate it before parsing and keeps parsing
// bounded by the HTTP handler's MaxBytesReader.
func UnmarshalProviderPayloadFor(provider, contentType string, data []byte) (InboundEmail, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if strings.HasPrefix(strings.ToLower(contentType), "application/json") || provider == "postmark" || provider == "generic" {
		return unmarshalJSONInbound(data)
	}
	values, attachments, err := parseFormPayload(contentType, data)
	if err != nil {
		return InboundEmail{}, ErrInvalidMessage
	}
	input := inboundFromValues(values)
	input.Attachments = attachments
	return input, nil
}

func unmarshalJSONInbound(data []byte) (InboundEmail, error) {
	var raw struct {
		To                json.RawMessage `json:"to"`
		OriginalRecipient string          `json:"OriginalRecipient"`
		From              string          `json:"from"`
		FromFull          struct {
			Email string `json:"Email"`
		} `json:"FromFull"`
		Subject           string   `json:"subject"`
		Body              string   `json:"body"`
		Text              string   `json:"text"`
		TextBody          string   `json:"TextBody"`
		StrippedTextReply string   `json:"StrippedTextReply"`
		MessageID         string   `json:"message_id"`
		MessageID2        string   `json:"MessageID"`
		InReplyTo         string   `json:"in_reply_to"`
		References        []string `json:"references"`
		Headers           []struct {
			Name  string `json:"Name"`
			Value string `json:"Value"`
		} `json:"Headers"`
		Attachments []struct {
			Name        string `json:"Name"`
			ContentType string `json:"ContentType"`
			Content     string `json:"Content"`
		} `json:"Attachments"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return InboundEmail{}, ErrInvalidMessage
	}
	to := addressesFromJSON(raw.To)
	if len(to) == 0 && raw.OriginalRecipient != "" {
		to = []string{raw.OriginalRecipient}
	}
	from := raw.From
	if from == "" {
		from = raw.FromFull.Email
	}
	body := firstNonEmpty(raw.StrippedTextReply, raw.TextBody, raw.Body, raw.Text)
	messageID := firstNonEmpty(raw.MessageID, raw.MessageID2)
	inReplyTo := raw.InReplyTo
	refs := raw.References
	for _, header := range raw.Headers {
		switch strings.ToLower(strings.TrimSpace(header.Name)) {
		case "message-id":
			messageID = firstNonEmpty(header.Value, messageID)
		case "in-reply-to":
			inReplyTo = firstNonEmpty(header.Value, inReplyTo)
		case "references":
			refs = append(refs, strings.Fields(header.Value)...)
		}
	}
	attachments := make([]InboundAttachment, 0, len(raw.Attachments))
	for _, attachment := range raw.Attachments {
		decoded, err := decodeAttachment(attachment.Content)
		if err != nil {
			return InboundEmail{}, ErrInvalidMessage
		}
		attachments = append(attachments, InboundAttachment{Name: attachment.Name, MIMEType: nonEmpty(attachment.ContentType, "application/octet-stream"), Body: decoded})
	}
	return InboundEmail{To: to, From: from, Subject: raw.Subject, Body: body, MessageID: messageID, InReplyTo: inReplyTo, References: uniqueStrings(refs), Attachments: attachments}, nil
}

func parseFormValues(contentType string, data []byte) (url.Values, error) {
	values, _, err := parseFormPayload(contentType, data)
	return values, err
}

func parseFormPayload(contentType string, data []byte) (url.Values, []InboundAttachment, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	switch strings.ToLower(mediaType) {
	case "application/x-www-form-urlencoded", "":
		values, err := url.ParseQuery(string(data))
		return values, nil, err
	case "multipart/form-data":
		boundary := params["boundary"]
		if boundary == "" {
			return nil, nil, ErrInvalidMessage
		}
		reader := multipart.NewReader(bytes.NewReader(data), boundary)
		values := make(url.Values)
		attachments := make([]InboundAttachment, 0)
		for {
			part, nextErr := reader.NextPart()
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if nextErr != nil {
				return nil, nil, nextErr
			}
			fileName := part.FileName()
			mimeType := part.Header.Get("Content-Type")
			value, readErr := io.ReadAll(io.LimitReader(part, (2<<20)+1))
			part.Close()
			if readErr != nil {
				return nil, nil, readErr
			}
			if len(value) > 2<<20 {
				return nil, nil, ErrInvalidMessage
			}
			if fileName != "" || strings.HasPrefix(strings.ToLower(part.FormName()), "attachment") {
				attachments = append(attachments, InboundAttachment{Name: fileName, MIMEType: nonEmpty(mimeType, "application/octet-stream"), Body: value})
			} else {
				values.Add(part.FormName(), string(value))
			}
		}
		return values, attachments, nil
	default:
		return nil, nil, ErrInvalidMessage
	}
}

func decodeAttachment(value string) ([]byte, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil || len(decoded) > 2<<20 {
		return nil, ErrInvalidMessage
	}
	return decoded, nil
}

func inboundFromValues(values url.Values) InboundEmail {
	headers := parseHeaderValues(firstNonEmpty(values.Get("headers"), values.Get("message-headers")))
	messageID := firstNonEmpty(values.Get("message_id"), values.Get("Message-Id"), values.Get("message-id"), headers["message-id"])
	inReplyTo := firstNonEmpty(values.Get("in_reply_to"), values.Get("In-Reply-To"), values.Get("in-reply-to"), headers["in-reply-to"])
	refs := append(strings.Fields(values.Get("references")), strings.Fields(headers["references"])...)
	return InboundEmail{
		To:         splitAddresses(firstNonEmpty(values.Get("to"), values.Get("recipient"), values.Get("OriginalRecipient"))),
		From:       firstNonEmpty(values.Get("from"), values.Get("sender")),
		Subject:    values.Get("subject"),
		Body:       firstNonEmpty(values.Get("body"), values.Get("body-plain"), values.Get("text"), values.Get("text_body")),
		MessageID:  messageID,
		InReplyTo:  inReplyTo,
		References: uniqueStrings(refs),
	}
}

// UnmarshalDeliveryPayload normalizes delivery/bounce callbacks from JSON or
// provider form posts. The caller still authenticates the raw request with
// the mailbox secret before applying the normalized event.
func UnmarshalDeliveryPayload(provider, contentType string, data []byte) (DeliveryEvent, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	var values url.Values
	if strings.HasPrefix(strings.ToLower(contentType), "application/json") || provider == "postmark" {
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			return DeliveryEvent{}, ErrInvalidDelivery
		}
		values = mapToValues(raw)
	} else {
		parsed, err := parseFormValues(contentType, data)
		if err != nil {
			return DeliveryEvent{}, ErrInvalidDelivery
		}
		values = parsed
	}
	typ, hard := normalizeDeliveryType(firstNonEmpty(values.Get("type"), values.Get("event"), values.Get("RecordType"), values.Get("record_type"), values.Get("bounce-type")), values.Get("hard"))
	if typ == "" {
		return DeliveryEvent{}, ErrInvalidDelivery
	}
	event := DeliveryEvent{
		ProviderEventID: firstNonEmpty(values.Get("provider_event_id"), values.Get("MessageID"), values.Get("message_id"), values.Get("id"), values.Get("event-id")),
		Type:            typ,
		Recipient:       firstNonEmpty(values.Get("recipient"), values.Get("Email"), values.Get("email"), values.Get("to")),
		MessageID:       firstNonEmpty(values.Get("message_id"), values.Get("MessageID"), values.Get("message-id")),
		BounceType:      firstNonEmpty(values.Get("bounce_type"), values.Get("Type"), values.Get("bounce-type")),
		Reason:          firstNonEmpty(values.Get("reason"), values.Get("Description"), values.Get("description"), values.Get("error")),
		Hard:            hard,
		OccurredAt:      parseProviderTime(firstNonEmpty(values.Get("occurred_at"), values.Get("BouncedAt"), values.Get("DeliveredAt"), values.Get("timestamp"))),
	}
	if event.ProviderEventID == "" {
		hash := sha256.Sum256(data)
		event.ProviderEventID = hex.EncodeToString(hash[:])
	}
	if event.Type == "bounced" && !event.Hard {
		event.Hard = strings.Contains(strings.ToLower(event.BounceType), "hard") || strings.Contains(strings.ToLower(event.Reason), "permanent")
	}
	return event, nil
}

func mapToValues(raw map[string]any) url.Values {
	values := make(url.Values)
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			values.Set(key, typed)
		case bool:
			values.Set(key, fmt.Sprint(typed))
		case float64:
			values.Set(key, fmt.Sprint(typed))
		}
	}
	return values
}

func normalizeDeliveryType(value, hard string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(value))
	hardBounce := strings.EqualFold(strings.TrimSpace(hard), "true") || strings.Contains(lower, "hard")
	switch {
	case strings.Contains(lower, "deliver") || lower == "sent":
		return "delivered", false
	case strings.Contains(lower, "bounce") || strings.Contains(lower, "complaint") || strings.Contains(lower, "spam"):
		return "bounced", hardBounce || strings.Contains(lower, "complaint") || strings.Contains(lower, "spam")
	case strings.Contains(lower, "defer") || strings.Contains(lower, "fail"):
		return "failed", false
	default:
		return "", false
	}
}

func parseProviderTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if unix, err := parseUnix(value); err == nil && unix > 0 {
		return time.Unix(unix, 0).UTC()
	}
	for _, layout := range []string{time.RFC3339, time.RFC1123Z, "Mon, 2 Jan 2006 15:04:05 -0700"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func parseHeaderValues(value string) map[string]string {
	result := make(map[string]string)
	value = strings.TrimSpace(value)
	if value == "" {
		return result
	}
	var pairs []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	}
	if json.Unmarshal([]byte(value), &pairs) == nil {
		for _, pair := range pairs {
			result[strings.ToLower(strings.TrimSpace(pair.Name))] = strings.TrimSpace(pair.Value)
		}
		return result
	}
	for _, line := range strings.Split(value, "\n") {
		name, headerValue, ok := strings.Cut(line, ":")
		if ok {
			result[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(headerValue)
		}
	}
	return result
}

func addressesFromJSON(raw json.RawMessage) []string {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return splitAddresses(single)
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		result := make([]string, 0, len(list))
		for _, item := range list {
			result = append(result, splitAddresses(item)...)
		}
		return result
	}
	return nil
}

func splitAddresses(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	addresses, err := mail.ParseAddressList(value)
	if err != nil {
		return []string{strings.TrimSpace(value)}
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, address.Address)
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
