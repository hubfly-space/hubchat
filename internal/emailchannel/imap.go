package emailchannel

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	messageMail "github.com/emersion/go-message/mail"
)

const (
	JobPollIMAP    = "email.imap_poll"
	IMAPPollEvery  = time.Minute
	imapMaxMessage = 2 << 20
)

// PollIMAP checks every enabled IMAP mailbox once. The worker records errors
// per mailbox so one unavailable provider does not prevent other workspaces
// from being polled. Message ingestion uses the same resolved path as signed
// webhooks, preserving sender policy, deduplication, threading, and file
// authorization.
func (s *Service) PollIMAP(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, inbox_id, address::text, inbound_mode,
		       imap_host, imap_port, imap_username, imap_password,
		       allowed_senders, blocked_senders, enabled
		FROM email_mailboxes
		WHERE inbound_mode='imap' AND enabled
		  AND imap_host IS NOT NULL AND imap_username IS NOT NULL AND imap_password IS NOT NULL
		ORDER BY workspace_id, id`)
	if err != nil {
		return 0, fmt.Errorf("emailchannel: list IMAP mailboxes: %w", err)
	}
	defer rows.Close()

	processed := 0
	for rows.Next() {
		var item imapMailbox
		var encryptedPassword []byte
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.InboxID, &item.Address, &item.InboundMode, &item.Host, &item.Port, &item.Username, &encryptedPassword, &item.AllowedSenders, &item.BlockedSenders, &item.Enabled); err != nil {
			return processed, fmt.Errorf("emailchannel: scan IMAP mailbox: %w", err)
		}
		password, decryptErr := s.decrypt(encryptedPassword)
		if decryptErr != nil {
			s.recordIMAPPollError(ctx, item.WorkspaceID, item.ID, decryptErr)
			continue
		}
		count, pollErr := s.pollMailbox(ctx, item, password)
		if pollErr != nil {
			s.recordIMAPPollError(ctx, item.WorkspaceID, item.ID, pollErr)
			continue
		}
		processed += count
		if _, updateErr := s.pool.Exec(ctx, `UPDATE email_mailboxes SET last_polled_at=now(),last_error=NULL WHERE workspace_id=$1 AND id=$2`, item.WorkspaceID, item.ID); updateErr != nil {
			return processed, fmt.Errorf("emailchannel: record IMAP poll: %w", updateErr)
		}
	}
	if err := rows.Err(); err != nil {
		return processed, fmt.Errorf("emailchannel: iterate IMAP mailboxes: %w", err)
	}
	return processed, nil
}

type imapMailbox struct {
	ID             string
	WorkspaceID    string
	InboxID        string
	Address        string
	InboundMode    string
	Host           *string
	Port           *int
	Username       *string
	AllowedSenders []string
	BlockedSenders []string
	Enabled        bool
}

func (s *Service) pollMailbox(ctx context.Context, mailbox imapMailbox, password string) (int, error) {
	if mailbox.Host == nil || strings.TrimSpace(*mailbox.Host) == "" || mailbox.Username == nil || strings.TrimSpace(*mailbox.Username) == "" {
		return 0, errors.New("emailchannel: IMAP host and username are required")
	}
	port := 993
	if mailbox.Port != nil && *mailbox.Port > 0 {
		port = *mailbox.Port
	}
	address := net.JoinHostPort(strings.TrimSpace(*mailbox.Host), strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn *client.Client
	var err error
	if port == 993 {
		conn, err = client.DialWithDialerTLS(dialer, address, &tls.Config{ServerName: strings.TrimSpace(*mailbox.Host), MinVersion: tls.VersionTLS12})
	} else {
		conn, err = client.DialWithDialer(dialer, address)
	}
	if err != nil {
		return 0, fmt.Errorf("emailchannel: connect IMAP %s: %w", address, err)
	}
	defer conn.Logout()
	conn.Timeout = 30 * time.Second
	if port != 993 {
		startTLS, supportErr := conn.SupportStartTLS()
		if supportErr != nil {
			return 0, fmt.Errorf("emailchannel: inspect IMAP TLS support: %w", supportErr)
		}
		if !startTLS {
			return 0, errors.New("emailchannel: refusing IMAP connection without TLS or STARTTLS")
		}
		if err := conn.StartTLS(&tls.Config{ServerName: strings.TrimSpace(*mailbox.Host), MinVersion: tls.VersionTLS12}); err != nil {
			return 0, fmt.Errorf("emailchannel: IMAP STARTTLS: %w", err)
		}
	}
	if err := conn.Login(strings.TrimSpace(*mailbox.Username), password); err != nil {
		return 0, fmt.Errorf("emailchannel: IMAP login: %w", err)
	}
	status, err := conn.Select(imap.InboxName, false)
	if err != nil {
		return 0, fmt.Errorf("emailchannel: select IMAP inbox: %w", err)
	}
	if status.Messages == 0 {
		return 0, nil
	}
	sequences, err := conn.Search(&imap.SearchCriteria{WithoutFlags: []string{imap.SeenFlag}})
	if err != nil {
		return 0, fmt.Errorf("emailchannel: search IMAP inbox: %w", err)
	}
	processed := 0
	for _, sequence := range sequences {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		message, raw, fetchErr := fetchIMAPMessage(conn, sequence)
		if fetchErr != nil {
			return processed, fetchErr
		}
		input, parseErr := parseIMAPMessage(raw, mailbox.Address, message.InternalDate)
		if parseErr != nil {
			return processed, fmt.Errorf("emailchannel: parse IMAP message %d: %w", sequence, parseErr)
		}
		_, ingestErr := s.ingestResolved(ctx, &Mailbox{ID: mailbox.ID, WorkspaceID: mailbox.WorkspaceID, InboxID: mailbox.InboxID, Address: mailbox.Address, InboundMode: mailbox.InboundMode, AllowedSenders: mailbox.AllowedSenders, BlockedSenders: mailbox.BlockedSenders, Enabled: mailbox.Enabled}, input)
		if ingestErr != nil && !errors.Is(ingestErr, ErrDuplicateMessage) && !errors.Is(ingestErr, ErrSenderBlocked) && !errors.Is(ingestErr, ErrInvalidMessage) {
			return processed, fmt.Errorf("emailchannel: ingest IMAP message %d: %w", sequence, ingestErr)
		}
		set := new(imap.SeqSet)
		set.AddNum(sequence)
		if err := conn.Store(set, imap.FormatFlagsOp(imap.SetFlags, false), []string{imap.SeenFlag}, nil); err != nil {
			return processed, fmt.Errorf("emailchannel: mark IMAP message %d seen: %w", sequence, err)
		}
		processed++
	}
	return processed, nil
}

func fetchIMAPMessage(conn *client.Client, sequence uint32) (*imap.Message, []byte, error) {
	set := new(imap.SeqSet)
	set.AddNum(sequence)
	section := &imap.BodySectionName{Peek: true}
	messages := make(chan *imap.Message, 1)
	if err := conn.Fetch(set, []imap.FetchItem{section.FetchItem(), imap.FetchInternalDate}, messages); err != nil {
		return nil, nil, fmt.Errorf("emailchannel: fetch IMAP message %d: %w", sequence, err)
	}
	message := <-messages
	if message == nil {
		return nil, nil, fmt.Errorf("emailchannel: IMAP message %d was not returned", sequence)
	}
	body := message.GetBody(section)
	if body == nil {
		return nil, nil, fmt.Errorf("emailchannel: IMAP message %d has no body", sequence)
	}
	raw, err := io.ReadAll(io.LimitReader(body, imapMaxMessage+1))
	if err != nil {
		return nil, nil, fmt.Errorf("emailchannel: read IMAP message %d: %w", sequence, err)
	}
	if len(raw) > imapMaxMessage {
		return nil, nil, ErrInvalidMessage
	}
	return message, raw, nil
}

func parseIMAPMessage(raw []byte, recipient string, receivedAt time.Time) (InboundEmail, error) {
	reader, err := messageMail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return InboundEmail{}, ErrInvalidMessage
	}
	defer reader.Close()
	from, err := reader.Header.AddressList("From")
	if err != nil || len(from) == 0 {
		return InboundEmail{}, ErrInvalidMessage
	}
	subject, _ := reader.Header.Subject()
	messageID, _ := reader.Header.MessageID()
	messageID = normalizeMessageID(messageID)
	inReplyTo := ""
	if values, headerErr := reader.Header.MsgIDList("In-Reply-To"); headerErr == nil && len(values) > 0 {
		inReplyTo = normalizeMessageID(values[0])
	}
	references, _ := reader.Header.MsgIDList("References")
	for index := range references {
		references[index] = normalizeMessageID(references[index])
	}
	input := InboundEmail{To: []string{recipient}, From: from[0].Address, Subject: subject, MessageID: messageID, InReplyTo: inReplyTo, References: uniqueStrings(references), ReceivedAt: receivedAt}
	var textBody string
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return InboundEmail{}, ErrInvalidMessage
		}
		contentType := "application/octet-stream"
		if inline, ok := part.Header.(*messageMail.InlineHeader); ok {
			contentType, _, _ = inline.ContentType()
		}
		if attachment, ok := part.Header.(*messageMail.AttachmentHeader); ok {
			contentType, _, _ = attachment.ContentType()
			name, nameErr := attachment.Filename()
			if nameErr != nil || strings.TrimSpace(name) == "" {
				name = "attachment"
			}
			body, readErr := io.ReadAll(io.LimitReader(part.Body, imapMaxMessage+1))
			if readErr != nil || len(body) > imapMaxMessage {
				return InboundEmail{}, ErrInvalidMessage
			}
			input.Attachments = append(input.Attachments, InboundAttachment{Name: name, MIMEType: contentType, Body: body})
			continue
		}
		if contentType == "text/plain" && textBody == "" {
			body, readErr := io.ReadAll(io.LimitReader(part.Body, imapMaxMessage+1))
			if readErr != nil || len(body) > imapMaxMessage {
				return InboundEmail{}, ErrInvalidMessage
			}
			textBody = string(body)
		}
	}
	input.Body = textBody
	if strings.TrimSpace(input.Body) == "" {
		return InboundEmail{}, ErrInvalidMessage
	}
	return input, nil
}

func normalizeMessageID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || (strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">")) {
		return value
	}
	return "<" + value + ">"
}

func (s *Service) recordIMAPPollError(ctx context.Context, workspaceID, mailboxID string, err error) {
	_, _ = s.pool.Exec(ctx, `UPDATE email_mailboxes SET last_polled_at=now(),last_error=left($3,2000) WHERE workspace_id=$1 AND id=$2`, workspaceID, mailboxID, err.Error())
}
