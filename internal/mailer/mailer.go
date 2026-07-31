// Package mailer sends transactional email.
//
// # Responsibilities
//
// Rendering the small set of messages the product sends unprompted — verify
// your address, reset your password, here is your sign-in link, you have been
// invited — and handing them to an SMTP server.
//
// # Boundary
//
// Outbound only, and only the messages authentication and membership depend
// on. The full email *channel* (§6.15: inbound ingestion, reply-by-email,
// threading, bounce handling) is a separate module; this one exists because
// sign-in flows cannot wait for it.
//
// Sending never blocks a request. §18 requires that an unavailable mail server
// degrade rather than fail: a queued message with a visible status is the
// correct outcome, not a 500 on the password-reset form.
package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"text/template"
	"time"

	"github.com/hubchat/hubchat/internal/config"
)

// Message is one email to send.
type Message struct {
	To      string
	Subject string
	// ReplyTo optionally overrides the configured global reply address. Email
	// channel messages use this for the mailbox that owns the conversation;
	// authentication mail leaves it empty and uses the configured value.
	ReplyTo string
	// Body is plain text. HTML mail is deliberately absent for now: these are
	// six short transactional messages, every client renders text, and it
	// removes a whole class of rendering and sanitisation concerns from the
	// authentication path.
	Body string
	// MessageID and InReplyTo are supplied by the email channel for replies.
	// Authentication messages leave them empty and remain ordinary standalone
	// mail.
	MessageID   string
	InReplyTo   string
	Attachments []Attachment
}

// Attachment is copied into the SMTP message at send time. The durable job
// stores file ids rather than bytes; the worker resolves and authorizes those
// ids immediately before delivery.
type Attachment struct {
	Name     string
	MIMEType string
	Body     []byte
}

// ErrNotConfigured is returned when no SMTP host is set.
//
// Callers treat it as a soft failure. A self-hosted instance with no mail
// server is a supported configuration (§8.1 lists SMTP as optional), and the
// interface's job is to say "we could not email you" rather than to pretend
// the account operation failed.
var ErrNotConfigured = errors.New("mailer: no SMTP server is configured")

// Sender delivers messages. An interface so tests can capture what would have
// been sent without standing up an SMTP server.
type Sender interface {
	Send(ctx context.Context, message Message) error
	Configured() bool
}

// SMTPSender talks to a real server.
type SMTPSender struct {
	cfg    config.Email
	logger *slog.Logger
	from   string
}

// New returns a Sender for the given configuration. Safe to construct even
// when email is unconfigured; Send then returns ErrNotConfigured.
func New(cfg config.Email, logger *slog.Logger) *SMTPSender {
	from := cfg.FromAddress
	if cfg.FromName != "" && from != "" {
		from = fmt.Sprintf("%s <%s>", cfg.FromName, from)
	}
	return &SMTPSender{cfg: cfg, logger: logger, from: from}
}

func (s *SMTPSender) Configured() bool {
	return s.cfg.Enabled && s.cfg.SMTPHost != "" && s.cfg.FromAddress != ""
}

// Send delivers one message synchronously.
//
// Callers that are on a request path should enqueue a job rather than calling
// this directly — see the `email.send` handler. It is exported for the job
// worker and for `hubchat doctor`.
func (s *SMTPSender) Send(ctx context.Context, message Message) error {
	if !s.Configured() {
		return ErrNotConfigured
	}

	addr := net.JoinHostPort(s.cfg.SMTPHost, fmt.Sprint(s.cfg.SMTPPort))

	// A deadline matters more than it looks: a mail server that accepts the
	// connection and then stalls would otherwise hold a worker goroutine
	// indefinitely.
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mailer: dial %s: %w", addr, err)
	}

	if s.cfg.Encryption == "tls" {
		conn = tls.Client(conn, &tls.Config{ServerName: s.cfg.SMTPHost})
	}

	client, err := smtp.NewClient(conn, s.cfg.SMTPHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mailer: smtp handshake: %w", err)
	}
	defer client.Close()

	if s.cfg.Encryption == "starttls" {
		// Only upgrade when the server advertises it. Demanding STARTTLS from
		// a local relay that does not offer it — MailHog, a sidecar — would
		// make the common self-hosted setup fail for no security gain over a
		// loopback connection.
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.cfg.SMTPHost}); err != nil {
				return fmt.Errorf("mailer: starttls: %w", err)
			}
		}
	}

	if s.cfg.SMTPUsername != "" {
		auth := smtp.PlainAuth("", s.cfg.SMTPUsername, s.cfg.SMTPPassword, s.cfg.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mailer: authenticate: %w", err)
		}
	}

	if err := client.Mail(s.cfg.FromAddress); err != nil {
		return fmt.Errorf("mailer: sender rejected: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("mailer: recipient rejected: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("mailer: open data: %w", err)
	}
	if _, err := writer.Write(s.compose(message)); err != nil {
		return fmt.Errorf("mailer: write body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("mailer: close body: %w", err)
	}

	return client.Quit()
}

// compose builds the RFC 5322 message.
//
// Header values are stripped of newlines before use. A display name or subject
// carrying a CR/LF would otherwise let a caller inject arbitrary headers — a
// Bcc, a different Reply-To — which for a message the *server* sends on a
// user's behalf is a real injection path.
func (s *SMTPSender) compose(message Message) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "From: %s\r\n", sanitizeHeader(s.from))
	fmt.Fprintf(&buf, "To: %s\r\n", sanitizeHeader(message.To))
	fmt.Fprintf(&buf, "Subject: %s\r\n", sanitizeHeader(message.Subject))
	replyTo := message.ReplyTo
	if replyTo == "" {
		replyTo = s.cfg.ReplyTo
	}
	if replyTo != "" {
		fmt.Fprintf(&buf, "Reply-To: %s\r\n", sanitizeHeader(replyTo))
	}
	if message.MessageID != "" {
		fmt.Fprintf(&buf, "Message-ID: %s\r\n", sanitizeHeader(message.MessageID))
	}
	if message.InReplyTo != "" {
		fmt.Fprintf(&buf, "In-Reply-To: %s\r\n", sanitizeHeader(message.InReplyTo))
		fmt.Fprintf(&buf, "References: %s\r\n", sanitizeHeader(message.InReplyTo))
	}
	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	buf.WriteString("MIME-Version: 1.0\r\n")
	// Transactional mail must never land in a "promotions" tab or be bounced
	// back to a list address.
	buf.WriteString("Auto-Submitted: auto-generated\r\n")
	if len(message.Attachments) == 0 {
		buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(strings.ReplaceAll(message.Body, "\n", "\r\n"))
		return buf.Bytes()
	}

	writer := multipart.NewWriter(&buf)
	boundary := writer.Boundary()
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n", boundary))
	buf.WriteString("\r\n")
	textHeader := make(textproto.MIMEHeader)
	textHeader.Set("Content-Type", "text/plain; charset=utf-8")
	textPart, err := writer.CreatePart(textHeader)
	if err != nil {
		return buf.Bytes()
	}
	_, _ = textPart.Write([]byte(strings.ReplaceAll(message.Body, "\n", "\r\n")))
	for _, attachment := range message.Attachments {
		name := sanitizeHeader(attachment.Name)
		if name == "" {
			name = "attachment"
		}
		mimeType := sanitizeHeader(attachment.MIMEType)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		contentType := mimeType
		if formatted := mime.FormatMediaType(mimeType, map[string]string{"name": name}); formatted != "" {
			contentType = formatted
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Type", contentType)
		header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		header.Set("Content-Transfer-Encoding", "base64")
		part, partErr := writer.CreatePart(header)
		if partErr != nil {
			continue
		}
		encoded := base64.StdEncoding.EncodeToString(attachment.Body)
		for len(encoded) > 0 {
			lineLength := 76
			if len(encoded) < lineLength {
				lineLength = len(encoded)
			}
			_, _ = part.Write([]byte(encoded[:lineLength] + "\r\n"))
			encoded = encoded[lineLength:]
		}
	}
	_ = writer.Close()

	return buf.Bytes()
}

func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}

// ---------------------------------------------------------------- templates

// Templates are text/template, not html/template, because the bodies are plain
// text. That is a deliberate pairing: switching one without the other is how
// an unescaped name ends up in an HTML mail.
var templates = template.Must(template.New("mail").Parse(`
{{define "verify_email"}}Hi {{.Name}},

Confirm this address to finish setting up your Hubchat account:

{{.Link}}

This link expires in {{.ExpiresIn}}. If you did not create an account, you can ignore this message.
{{end}}

{{define "reset_password"}}Hi {{.Name}},

Someone asked to reset the password for this Hubchat account. To choose a new one:

{{.Link}}

This link expires in {{.ExpiresIn}} and can be used once.

If it was not you, nothing has changed and you can ignore this message. Your password stays as it is.
{{end}}

{{define "magic_link"}}Hi {{.Name}},

Here is your sign-in link for Hubchat:

{{.Link}}

It expires in {{.ExpiresIn}} and can be used once. If you did not ask to sign in, you can ignore this message.
{{end}}

{{define "workspace_invite"}}Hi,

{{.InviterName}} invited you to join the {{.WorkspaceName}} workspace on Hubchat as {{.RoleLabel}}.

{{.Link}}

This invitation expires in {{.ExpiresIn}}.
{{end}}
`))

// Data carries everything the templates read. One struct for all of them so a
// renamed field breaks the build rather than silently rendering blank.
type Data struct {
	Name          string
	Link          string
	ExpiresIn     string
	InviterName   string
	WorkspaceName string
	RoleLabel     string
}

// Render produces a body from a named template.
func Render(name string, data Data) (string, error) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("mailer: render %s: %w", name, err)
	}
	return strings.TrimSpace(buf.String()) + "\n", nil
}
