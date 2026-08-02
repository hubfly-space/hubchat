package api

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/hubchat/hubchat/internal/mailer"
)

type userContextKey int

const currentUserKey userContextKey = 0

// requireUser resolves the session to a user, without requiring a workspace.
//
// Distinct from requireActor, which additionally resolves membership. Account
// settings — your password, your sessions, your second factor — belong to the
// person, not to a workspace, and gating them on membership would lock a user
// out of their own security settings the moment they were removed from their
// last workspace.
func requireUser(deps Deps, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httpserver.SessionToken(r)
		if token == "" {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized,
				"Sign in to continue.")
			return
		}

		user, err := deps.Auth.UserForSession(r.Context(), token)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized,
				"Your session has expired. Sign in again.")
			return
		}

		next(w, r.WithContext(context.WithValue(r.Context(), currentUserKey, user)))
	}
}

// userFromRequest returns the user requireUser attached. Only safe to call
// from a handler mounted behind it.
func userFromRequest(r *http.Request) *auth.User {
	user, _ := r.Context().Value(currentUserKey).(*auth.User)
	return user
}

// link builds an absolute URL into a browser surface, carrying one parameter.
//
// Built from the configured public URL rather than the request's Host header:
// a link in an email is followed later, out of band, and deriving it from a
// header an attacker controls is how a password-reset link ends up pointing at
// their server (§11.4).
func (d Deps) link(path string, key, value string) string {
	base := d.PublicURL
	if base == nil {
		return path
	}

	target := *base
	target.Path = strings.TrimSuffix(target.Path, "/") + path

	params := url.Values{}
	params.Set(key, value)
	target.RawQuery = params.Encode()

	return target.String()
}

// pathLink builds an absolute URL with no query string, for links whose token
// is already part of the path (an invite link's /invite/{token}, as opposed
// to a reset link's ?token=...).
func (d Deps) pathLink(path string) string {
	base := d.PublicURL
	if base == nil {
		return path
	}

	target := *base
	target.Path = strings.TrimSuffix(target.Path, "/") + path
	target.RawQuery = ""

	return target.String()
}

// issuerName is what an authenticator app shows next to the account.
func (d Deps) issuerName() string {
	if d.PublicURL != nil && d.PublicURL.Host != "" {
		return "Hubchat (" + d.PublicURL.Host + ")"
	}
	return "Hubchat"
}

// sendMail enqueues a message rather than sending it inline.
//
// §18 requires an unavailable mail server to degrade rather than fail: a
// password-reset request must not return 500 because SMTP is down. Queuing
// also means a slow relay cannot hold an HTTP request open — the durable
// queue's retry and dead-letter handling take over from here.
func (d Deps) sendMail(r *http.Request, to, subject, template string, data mailer.Data) {
	d.sendMailForWorkspace(r, "", to, subject, template, data)
}

// sendMailForWorkspace attaches tenant context to customer-facing messages
// whose source resource belongs to a workspace. Account-level authentication
// mail remains unscoped through sendMail above; portal and support messages
// must be visible to that workspace's job and delivery diagnostics.
func (d Deps) sendMailForWorkspace(r *http.Request, workspaceID, to, subject, template string, data mailer.Data) {
	body, err := mailer.Render(template, data)
	if err != nil {
		d.Logger.Error("rendering email failed", "template", template, "error", err)
		return
	}

	if d.Jobs == nil {
		d.Logger.Warn("no job queue configured; email not sent", "template", template)
		return
	}

	_, err = d.Jobs.Enqueue(r.Context(), jobs.Spec{
		WorkspaceID: workspaceID,
		Type:        JobEmailSend,
		Queue:       "email",
		Payload: EmailPayload{
			To:      to,
			Subject: subject,
			Body:    body,
		},
	})
	if err != nil {
		d.Logger.Error("queueing email failed", "template", template, "error", err)
	}
}

// sendCustomerTemplateMailForWorkspace queues a workspace-customizable
// customer message. Authentication mail deliberately continues through
// sendMailForWorkspace and the fixed internal/mailer templates.
func (d Deps) sendCustomerTemplateMailForWorkspace(r *http.Request, workspaceID, to, key, fallbackSubject, fallbackBody string, values map[string]string) {
	if d.Jobs == nil {
		return
	}
	_, err := d.Jobs.Enqueue(r.Context(), jobs.Spec{
		WorkspaceID: workspaceID, Queue: "email", Type: JobEmailSend,
		Payload: EmailPayload{To: to, Subject: fallbackSubject, Body: fallbackBody, WorkspaceID: workspaceID, TemplateKey: key, TemplateData: values},
	})
	if err != nil {
		d.Logger.Error("queueing customer template email failed", "template", key, "error", err)
	}
}

// JobEmailSend is the job type the worker registers for outbound mail.
const JobEmailSend = "email.send"

// EmailPayload is what the email.send job carries.
type EmailPayload struct {
	To             string            `json:"to"`
	Subject        string            `json:"subject"`
	Body           string            `json:"body"`
	ReplyTo        string            `json:"reply_to,omitempty"`
	MessageID      string            `json:"message_id,omitempty"`
	InReplyTo      string            `json:"in_reply_to,omitempty"`
	AttachmentIDs  []string          `json:"attachment_ids,omitempty"`
	WorkspaceID    string            `json:"workspace_id,omitempty"`
	EmailMessageID string            `json:"email_message_id,omitempty"`
	TemplateKey    string            `json:"template_key,omitempty"`
	TemplateData   map[string]string `json:"template_data,omitempty"`
}

// recordUserAudit writes an audit entry for an account-level action.
//
// Account actions are not workspace-scoped, but `audit_logs` is. The entry is
// written against every workspace the user belongs to, because "this person
// changed their password" is something each of their workspaces' owners may
// legitimately need to see — and there is no other place it would appear.
func (d Deps) recordUserAudit(r *http.Request, action audit.Action, userID string) {
	if d.Audit == nil {
		return
	}

	workspaceIDs, err := d.Workspace.WorkspaceIDsForUser(r.Context(), userID)
	if err != nil {
		d.Logger.Error("resolving workspaces for audit failed", "error", err)
		return
	}

	user := userFromRequest(r)
	name := ""
	if user != nil {
		name = user.Name
	}

	ip, _ := netip.ParseAddr(clientIP(r))

	for _, workspaceID := range workspaceIDs {
		entry := audit.Entry{
			WorkspaceID: workspaceID,
			ActorType:   audit.ActorUser,
			ActorID:     userID,
			ActorName:   name,
			Action:      action,
			EntityType:  "user",
			EntityID:    userID,
			RequestID:   httpserver.RequestIDFrom(r.Context()),
			IP:          ip,
		}
		if err := d.Audit.Record(r.Context(), entry); err != nil {
			d.Logger.Error("writing audit entry failed", "action", action, "error", err)
		}
	}
}
