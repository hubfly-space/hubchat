// Package emailchannel owns workspace mailboxes and inbound email threading.
// Outbound delivery is queued by the worker; this package records the durable
// message contract and resolves replies to conversations without relying on
// subject-line matching.
package emailchannel

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
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
)

type Service struct {
	pool         *database.Pool
	conversation *conversation.Service
	customer     *customer.Service
	inbox        *inbox.Service
	key          []byte
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
	To         []string  `json:"to"`
	From       string    `json:"from"`
	Subject    string    `json:"subject"`
	Body       string    `json:"body"`
	MessageID  string    `json:"message_id"`
	InReplyTo  string    `json:"in_reply_to"`
	References []string  `json:"references"`
	ReceivedAt time.Time `json:"received_at"`
}

type IngestResult struct {
	EmailMessageID string `json:"email_message_id"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Created        bool   `json:"created"`
}

func New(pool *database.Pool, secretKey []byte, conversationService *conversation.Service, customerService *customer.Service, inboxService *inbox.Service) *Service {
	key := sha256.Sum256(append([]byte("hubchat/email-secrets:"), secretKey...))
	return &Service{pool: pool, key: key[:], conversation: conversationService, customer: customerService, inbox: inboxService}
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
	_, err = s.pool.Exec(ctx, `INSERT INTO email_messages(id,workspace_id,mailbox_id,message_id_header,in_reply_to,references_headers,from_address,to_addresses,subject,status,received_at) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,NULLIF($9,''),'pending',$10)`, emailID, mailbox.WorkspaceID, mailbox.ID, strings.TrimSpace(input.MessageID), strings.TrimSpace(input.InReplyTo), input.References, from, input.To, strings.TrimSpace(input.Subject), input.ReceivedAt)
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
	_, err = s.pool.Exec(ctx, `UPDATE email_messages SET message_id=$3,conversation_id=$4,status='received',error=NULL WHERE workspace_id=$1 AND id=$2`, mailbox.WorkspaceID, emailID, messageID, conversationID)
	if err != nil {
		markFailed(err)
		return nil, err
	}
	return &IngestResult{EmailMessageID: emailID, ConversationID: conversationID, MessageID: messageID, Created: threadID == ""}, nil
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

// UnmarshalProviderPayload accepts the small common denominator used by
// inbound providers, while keeping provider-specific adapters possible later.
func UnmarshalProviderPayload(data []byte) (InboundEmail, error) {
	var raw struct {
		To         json.RawMessage `json:"to"`
		From       string          `json:"from"`
		Subject    string          `json:"subject"`
		Body       string          `json:"body"`
		Text       string          `json:"text"`
		MessageID  string          `json:"message_id"`
		MessageID2 string          `json:"MessageID"`
		InReplyTo  string          `json:"in_reply_to"`
		References []string        `json:"references"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return InboundEmail{}, ErrInvalidMessage
	}
	to := make([]string, 0)
	var single string
	if json.Unmarshal(raw.To, &single) == nil && single != "" {
		to = append(to, single)
	} else {
		_ = json.Unmarshal(raw.To, &to)
	}
	body := raw.Body
	if body == "" {
		body = raw.Text
	}
	messageID := raw.MessageID
	if messageID == "" {
		messageID = raw.MessageID2
	}
	return InboundEmail{To: to, From: raw.From, Subject: raw.Subject, Body: body, MessageID: messageID, InReplyTo: raw.InReplyTo, References: raw.References}, nil
}
