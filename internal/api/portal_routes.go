package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/mailer"
	"github.com/hubchat/hubchat/internal/portal"
	"github.com/hubchat/hubchat/internal/ticket"
)

func registerPortalRoutes(mux *http.ServeMux, deps Deps) {
	idempotent := Idempotency(deps)
	mux.HandleFunc("GET /v1/portal/bootstrap", handlePortalBootstrap(deps))
	mux.HandleFunc("POST /v1/portal/auth/magic-link", handlePortalMagicLink(deps))
	mux.HandleFunc("POST /v1/portal/auth/magic-link/redeem", handlePortalMagicLinkRedeem(deps))
	mux.HandleFunc("POST /v1/portal/auth/logout", handlePortalLogout(deps))
	mux.HandleFunc("GET /v1/portal/me", handlePortalMe(deps))
	mux.HandleFunc("PATCH /v1/portal/me", idempotent(handlePortalProfileUpdate(deps)))
	mux.HandleFunc("GET /v1/portal/me/export", handlePortalCustomerExport(deps))
	mux.HandleFunc("POST /v1/portal/me/delete", idempotent(handlePortalCustomerDelete(deps)))
	mux.HandleFunc("GET /v1/portal/tickets", handlePortalTickets(deps))
	mux.HandleFunc("POST /v1/portal/tickets", Idempotency(deps)(handlePortalCreateTicket(deps)))
	mux.HandleFunc("GET /v1/portal/tickets/{id}", handlePortalTicket(deps))
	mux.HandleFunc("POST /v1/portal/tickets/{id}/files", Idempotency(deps)(handlePortalTicketFileUpload(deps)))
	mux.HandleFunc("POST /v1/portal/tickets/{id}/replies", idempotent(handlePortalTicketReply(deps)))
	mux.HandleFunc("GET /v1/portal/files/{id}", handlePortalFileDownload(deps))
}

type portalBootstrapResponse struct {
	Portal      map[string]any                  `json:"portal"`
	Viewer      *portal.Customer                `json:"viewer"`
	Preferences *portal.NotificationPreferences `json:"preferences,omitempty"`
	Session     *portalSessionJSON              `json:"session,omitempty"`
}

type portalSessionJSON struct {
	PortalID   string `json:"portal_id"`
	ExpiresAt  string `json:"expires_at"`
	AuthMethod string `json:"auth_method"`
}

func handlePortalBootstrap(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := resolvePortal(deps, r)
		if err != nil {
			writePortalError(w, r, err)
			return
		}
		response := portalBootstrapResponse{Portal: portalJSON(*p)}
		if token := httpserver.PortalSessionToken(r); token != "" {
			session, sessionErr := deps.Portal.Session(r.Context(), token, p.ID)
			if sessionErr == nil {
				response, err = portalSessionResponse(r.Context(), deps, session)
				if err != nil {
					writePortalError(w, r, err)
					return
				}
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, response)
	}
}

type portalMagicLinkRequest struct {
	Portal string `json:"portal"`
	Email  string `json:"email"`
	Next   string `json:"next"`
}

func handlePortalMagicLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req portalMagicLinkRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Email) == "" {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Enter a valid email address.")
			return
		}
		p, err := resolvePortalByIdentifier(deps, r, req.Portal)
		if err == nil {
			link, issueErr := deps.Portal.IssueMagicLink(r.Context(), p.ID, req.Email)
			if issueErr == nil {
				deps.sendMailForWorkspace(r, p.WorkspaceID, link.Customer.Email, "Your Hubchat portal sign-in link", "magic_link", mailer.Data{
					Name: link.Customer.Name, Link: portalMagicLink(deps, p.ID, link.Token, safePortalNext(req.Next)), ExpiresIn: "15 minutes",
				})
			} else if !errors.Is(issueErr, portal.ErrCustomerNotFound) && !errors.Is(issueErr, portal.ErrForbidden) {
				deps.Logger.Error("issuing portal magic link failed", "error", issueErr)
			}
		} else if !errors.Is(err, portal.ErrNotFound) && !errors.Is(err, portal.ErrNotConfigured) && !errors.Is(err, portal.ErrPortalRequired) {
			deps.Logger.Error("resolving portal for magic link failed", "error", err)
		}
		// Do not reveal whether the address belongs to a customer.
		httpserver.WriteJSON(w, http.StatusAccepted, map[string]any{"acknowledged": true})
	}
}

type portalTokenRequest struct {
	Token string `json:"token"`
}

func handlePortalMagicLinkRedeem(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req portalTokenRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Token) == "" {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "This sign-in link is incomplete.")
			return
		}
		session, err := deps.Portal.RedeemMagicLink(r.Context(), req.Token, r.UserAgent(), clientIP(r))
		if err != nil {
			writePortalError(w, r, err)
			return
		}
		httpserver.SetPortalSessionCookie(w, session.Token, secondsUntil(session.ExpiresAt), deps.CookieDomain, deps.CookieSecure)
		response, responseErr := portalSessionResponse(r.Context(), deps, session)
		if responseErr != nil {
			writePortalError(w, r, responseErr)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, response)
	}
}

func handlePortalLogout(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token := httpserver.PortalSessionToken(r); token != "" {
			if err := deps.Portal.Logout(r.Context(), token); err != nil {
				deps.Logger.Warn("revoking portal session failed", "error", err)
			}
		}
		httpserver.ClearPortalSessionCookie(w, deps.CookieDomain, deps.CookieSecure)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handlePortalMe(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		preferences, err := deps.Portal.Preferences(r.Context(), session.WorkspaceID, session.CustomerID)
		if err != nil {
			writePortalError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"viewer": session.Customer, "portal": portalJSON(*session.Portal), "preferences": preferences})
	}
}

func handlePortalProfileUpdate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		var input portal.ProfileInput
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed profile update.")
			return
		}
		viewer, err := deps.Portal.UpdateProfile(r.Context(), session, input)
		if err != nil {
			writePortalError(w, r, err)
			return
		}
		preferences, err := deps.Portal.Preferences(r.Context(), session.WorkspaceID, session.CustomerID)
		if err != nil {
			writePortalError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"viewer": viewer, "preferences": preferences})
	}
}

func handlePortalCustomerExport(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		bundle, err := deps.Customer.Export(r.Context(), session.WorkspaceID, session.CustomerID)
		if err != nil {
			if errors.Is(err, customer.ErrNotFound) {
				httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Your customer profile is no longer available.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not prepare your data export.")
			return
		}
		if deps.Audit != nil {
			if err := deps.Audit.Record(r.Context(), audit.Entry{
				WorkspaceID: session.WorkspaceID, ActorType: audit.ActorCustomer, ActorID: session.CustomerID,
				Action: audit.DataExported, EntityType: "customer", EntityID: session.CustomerID,
			}); err != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not record the data export.")
				return
			}
		}
		w.Header().Set("Content-Disposition", `attachment; filename="hubchat-customer-export.json"`)
		httpserver.WriteJSON(w, http.StatusOK, bundle)
	}
}

func handlePortalCustomerDelete(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		var input struct {
			Confirmation string `json:"confirmation"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil || strings.TrimSpace(input.Confirmation) != "DELETE" {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "Type DELETE to confirm account anonymisation.")
			return
		}
		// Revoke first. If this fails, the anonymisation does not begin and the
		// user can retry without leaving an active session behind.
		if err := deps.Portal.RevokeCustomerSessions(r.Context(), session.WorkspaceID, session.CustomerID); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not revoke your portal sessions.")
			return
		}
		if err := deps.Customer.DeleteAsCustomer(r.Context(), session.WorkspaceID, session.CustomerID); err != nil {
			if errors.Is(err, customer.ErrNotFound) {
				httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Your customer profile is no longer available.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not anonymise your account.")
			return
		}
		httpserver.ClearPortalSessionCookie(w, deps.CookieDomain, deps.CookieSecure)
		w.WriteHeader(http.StatusNoContent)
	}
}

func portalSessionResponse(ctx context.Context, deps Deps, session *portal.Session) (portalBootstrapResponse, error) {
	preferences, err := deps.Portal.Preferences(ctx, session.WorkspaceID, session.CustomerID)
	if err != nil {
		return portalBootstrapResponse{}, err
	}
	return portalBootstrapResponse{
		Portal: portalJSON(*session.Portal), Viewer: &session.Customer, Preferences: preferences,
		Session: &portalSessionJSON{PortalID: session.PortalID, ExpiresAt: session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"), AuthMethod: session.AuthMethod},
	}, nil
}

func handlePortalTickets(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		tickets, err := deps.Portal.Tickets(r.Context(), session, portal.TicketFilter{Before: cursor.At, BeforeID: cursor.ID, Limit: limit + 1})
		if err != nil {
			deps.Logger.Error("portal ticket list failed", "error", err)
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load your requests.")
			return
		}
		page := NewPage(tickets, limit, func(t portal.Ticket) Cursor { return Cursor{At: t.UpdatedAt, ID: t.ID} })
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

type portalCreateTicketRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

func handlePortalCreateTicket(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		if session.Portal.DefaultInboxID == nil {
			httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "This portal has no support inbox configured.")
			return
		}
		var req portalCreateTicketRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		customerID := session.CustomerID
		conversationSubject := strings.TrimSpace(req.Title)
		conversationBody := strings.TrimSpace(req.Description)
		if conversationSubject == "" || conversationBody == "" {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationError, "A title and description are required.")
			return
		}
		conv, openingMessage, err := deps.Conversation.Start(r.Context(), session.WorkspaceID, *session.Portal.DefaultInboxID, "portal", &conversationSubject, &customerID, nil, session.Customer.Name, conversationBody)
		if err != nil {
			if errors.Is(err, conversation.ErrEmptyBody) {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationError, "Describe what you need help with.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not start your request.")
			return
		}
		priority := portalTicketPriority(req.Priority)
		ticket, err := deps.Ticket.CreateAsCustomer(r.Context(), session.WorkspaceID, customerID, session.Customer.Name, ticket.CreateRequest{
			Title: req.Title, Description: req.Description, Priority: priority, CustomerID: &customerID,
			InboxID: *session.Portal.DefaultInboxID, Channel: "portal", ConversationID: &conv.ID,
		})
		if err != nil {
			deps.Logger.Error("creating portal ticket failed after conversation creation", "error", err, "conversation_id", conv.ID)
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not create your request.")
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{
			"ticket":             portalTicketJSON(*ticket),
			"opening_message_id": openingMessage.ID,
		})
	}
}

func handlePortalTicket(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")
		allowed, err := deps.Portal.CanAccessTicket(r.Context(), session, id)
		if err != nil || !allowed {
			writePortalNotFound(w, r)
			return
		}
		ticket, err := deps.Ticket.Get(r.Context(), session.WorkspaceID, id)
		if err != nil {
			writePortalNotFound(w, r)
			return
		}
		before, after, limit, pageErr := messagePageParams(r)
		if pageErr != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, pageErr.Error())
			return
		}
		messages := []conversation.Message{}
		hasMore := false
		if ticket.ConversationID != nil {
			var messageErr error
			messages, hasMore, messageErr = deps.Conversation.ListMessagesPage(r.Context(), session.WorkspaceID, *ticket.ConversationID, before, after, limit)
			if messageErr != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load this request.")
				return
			}
		}
		outMessages := make([]map[string]any, 0, len(messages))
		for _, message := range messages {
			if message.Kind == "reply" {
				outMessages = append(outMessages, portalMessageJSONWithAttachments(r, deps, session.WorkspaceID, message))
			}
		}
		out := map[string]any{"ticket": portalTicketJSON(*ticket), "messages": outMessages, "has_more": hasMore, "next_cursor": nil}
		if hasMore && len(messages) > 0 && after == 0 {
			cursor := Cursor{Value: strconv.FormatInt(messages[0].Sequence, 10)}.Encode()
			out["next_cursor"] = cursor
		}
		httpserver.WriteJSON(w, http.StatusOK, out)
	}
}

func portalTicketJSON(t ticket.Ticket) map[string]any {
	return map[string]any{
		"id": t.ID, "number": t.Number, "prefix": t.Prefix, "title": t.Title,
		"description": t.Description, "status": t.Status, "priority": t.Priority,
		"conversation_id": t.ConversationID, "created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
	}
}

func portalMessageJSON(m conversation.Message) map[string]any {
	return portalMessageJSONWithAttachments(nil, Deps{}, "", m)
}

func portalMessageJSONWithAttachments(r *http.Request, deps Deps, workspaceID string, m conversation.Message) map[string]any {
	attachments := []map[string]any{}
	if r != nil && deps.File != nil {
		if records, err := deps.File.MessageAttachments(r.Context(), workspaceID, m.ID); err == nil {
			for _, record := range records {
				attachments = append(attachments, portalFileJSON(record))
			}
		}
	}
	return map[string]any{
		"id": m.ID, "conversation_id": m.ConversationID, "kind": m.Kind,
		"author_type": m.AuthorType, "author_id": m.AuthorID, "author_name": m.AuthorName,
		"body": m.Body, "delivery": m.Delivery, "sequence": m.Sequence,
		"edited_at": m.EditedAt, "created_at": m.CreatedAt,
		"attachments": attachments,
	}
}

type portalReplyRequest struct {
	Body     string   `json:"body"`
	ClientID *string  `json:"client_id"`
	FileIDs  []string `json:"file_ids"`
}

func handlePortalTicketReply(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")
		allowed, err := deps.Portal.CanAccessTicket(r.Context(), session, id)
		if err != nil || !allowed {
			writePortalNotFound(w, r)
			return
		}
		ticket, err := deps.Ticket.Get(r.Context(), session.WorkspaceID, id)
		if err != nil || ticket.ConversationID == nil {
			writePortalNotFound(w, r)
			return
		}
		var req portalReplyRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		customerID := session.CustomerID
		message, err := deps.Conversation.PostMessage(r.Context(), session.WorkspaceID, *ticket.ConversationID,
			req.ClientID, "reply", "customer", &customerID, session.Customer.Name, req.Body)
		if err != nil {
			if errors.Is(err, conversation.ErrEmptyBody) {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationError, "Reply cannot be empty.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not send your reply.")
			return
		}
		if deps.File != nil && len(req.FileIDs) > 0 {
			if attachErr := deps.File.AttachToMessage(r.Context(), session.WorkspaceID, message.ID, req.FileIDs); attachErr != nil {
				if errors.Is(attachErr, file.ErrInvalidAttachment) {
					httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "One or more attachments are not available.")
				} else {
					httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "The reply was sent, but its attachments could not be linked.")
				}
				return
			}
		}
		if deps.Notification != nil {
			if notifyErr := deps.Notification.NotifyConversationMessage(r.Context(), session.WorkspaceID, *ticket.ConversationID,
				message.ID, message.AuthorType, "", message.Body); notifyErr != nil && deps.Logger != nil {
				deps.Logger.Warn("could not create portal reply notification", "ticket_id", id, "error", notifyErr)
			}
		}
		httpserver.WriteJSON(w, http.StatusCreated, portalMessageJSONWithAttachments(r, deps, session.WorkspaceID, *message))
	}
}

func handlePortalTicketFileUpload(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		ticketID := r.PathValue("id")
		allowed, err := deps.Portal.CanAccessTicket(r.Context(), session, ticketID)
		if err != nil || !allowed {
			writePortalNotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "The upload could not be read.")
			return
		}
		parts := r.MultipartForm.File["file"]
		if len(parts) != 1 {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Upload exactly one file.")
			return
		}
		part := parts[0]
		opened, err := part.Open()
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "The upload could not be opened.")
			return
		}
		defer opened.Close()
		mimeType := part.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = mime.TypeByExtension(filepath.Ext(part.Filename))
		}
		created, err := deps.File.Create(r.Context(), session.WorkspaceID, file.UploadInput{
			Name: filepath.Base(part.Filename), MIMEType: mimeType, SizeBytes: part.Size, Body: opened,
			OwnerType: "ticket", OwnerID: ticketID, UploadedByType: "customer", UploadedByID: session.CustomerID,
		})
		if err != nil {
			writeFileError(w, r, err)
			return
		}
		if messageID := strings.TrimSpace(r.FormValue("message_id")); messageID != "" {
			ticket, ticketErr := deps.Ticket.Get(r.Context(), session.WorkspaceID, ticketID)
			if ticketErr != nil || ticket.ConversationID == nil {
				_ = deps.File.Delete(r.Context(), session.WorkspaceID, created.ID)
				writePortalNotFound(w, r)
				return
			}
			var messageBelongs bool
			if err := deps.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM messages WHERE workspace_id=$1 AND id=$2 AND conversation_id=$3)`, session.WorkspaceID, messageID, *ticket.ConversationID).Scan(&messageBelongs); err != nil || !messageBelongs {
				_ = deps.File.Delete(r.Context(), session.WorkspaceID, created.ID)
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "The attachment target is invalid.")
				return
			}
			if err := deps.File.AttachToMessage(r.Context(), session.WorkspaceID, messageID, []string{created.ID}); err != nil {
				_ = deps.File.Delete(r.Context(), session.WorkspaceID, created.ID)
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "The attachment could not be linked.")
				return
			}
		}
		httpserver.WriteJSON(w, http.StatusCreated, portalFileJSON(*created))
	}
}

func handlePortalFileDownload(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := requirePortalSession(deps, w, r)
		if !ok {
			return
		}
		record, opened, err := deps.File.Open(r.Context(), session.WorkspaceID, r.PathValue("id"))
		if err != nil || record.OwnerType != "ticket" {
			writePortalNotFound(w, r)
			return
		}
		allowed, accessErr := deps.Portal.CanAccessTicket(r.Context(), session, record.OwnerID)
		if accessErr != nil || !allowed {
			opened.Close()
			writePortalNotFound(w, r)
			return
		}
		defer opened.Close()
		w.Header().Set("Content-Type", record.MIMEType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", record.SizeBytes))
		w.Header().Set("Content-Disposition", contentDisposition(record.Name))
		_, _ = io.Copy(w, opened)
	}
}

func portalFileJSON(record file.Record) map[string]any {
	return map[string]any{
		"id": record.ID, "name": record.Name, "mime_type": record.MIMEType, "size_bytes": record.SizeBytes,
		"url": "/api/v1/portal/files/" + record.ID,
	}
}

func requirePortalSession(deps Deps, w http.ResponseWriter, r *http.Request) (*portal.Session, bool) {
	token := httpserver.PortalSessionToken(r)
	if token == "" {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Sign in to continue.")
		return nil, false
	}
	portalID := portalIdentifier(r)
	if portalID == "" {
		if resolved, resolveErr := resolvePortal(deps, r); resolveErr == nil {
			portalID = resolved.ID
		}
	}
	session, err := deps.Portal.Session(r.Context(), token, portalID)
	if err != nil {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Your portal session has expired.")
		return nil, false
	}
	return session, true
}

func resolvePortal(deps Deps, r *http.Request) (*portal.Portal, error) {
	return resolvePortalByIdentifier(deps, r, portalIdentifier(r))
}

func resolvePortalByIdentifier(deps Deps, r *http.Request, identifier string) (*portal.Portal, error) {
	if deps.Portal == nil {
		return nil, portal.ErrNotConfigured
	}
	identifier = strings.TrimSpace(identifier)
	if identifier != "" {
		return deps.Portal.Resolve(r.Context(), identifier)
	}
	host := r.Host
	if hostname, _, err := net.SplitHostPort(host); err == nil {
		host = hostname
	}
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host != "" {
		if resolved, err := deps.Portal.Resolve(r.Context(), host); err == nil {
			return resolved, nil
		}
	}
	return deps.Portal.Resolve(r.Context(), "")
}

func portalIdentifier(r *http.Request) string {
	if value := r.URL.Query().Get("portal"); value != "" {
		return value
	}
	return r.Header.Get("Hubchat-Portal-Id")
}

func portalJSON(p portal.Portal) map[string]any {
	domains := make([]map[string]any, 0, len(p.Domains))
	for _, item := range p.Domains {
		domains = append(domains, map[string]any{"id": item.ID, "portal_id": item.PortalID, "domain": item.Domain, "status": item.Status, "verified_at": item.VerifiedAt, "last_checked_at": item.LastCheckedAt})
	}
	return map[string]any{
		"id": p.ID, "workspace_id": p.WorkspaceID, "name": p.Name, "subdomain": p.Subdomain,
		"theme": p.Theme, "features": p.Features, "auth_methods": p.AuthMethods,
		"permissions": p.Permissions, "default_language": p.DefaultLanguage,
		"navigation": p.Navigation, "domains": domains, "enabled": p.Enabled,
	}
}

func portalMagicLink(deps Deps, portalID, token, next string) string {
	if deps.PublicURL == nil {
		link := fmt.Sprintf("/portal/sign-in?portal=%s&token=%s", url.QueryEscape(portalID), url.QueryEscape(token))
		if next != "" {
			link += "&next=" + url.QueryEscape(next)
		}
		return link
	}
	target := *deps.PublicURL
	target.Path = strings.TrimSuffix(target.Path, "/") + "/portal/sign-in"
	query := target.Query()
	query.Set("portal", portalID)
	query.Set("token", token)
	if next != "" {
		query.Set("next", next)
	}
	target.RawQuery = query.Encode()
	return target.String()
}

// safePortalNext only accepts a path handled by the portal bundle. It is
// intentionally validated before being placed in a customer email so a
// caller cannot turn the magic-link flow into an open redirect.
func safePortalNext(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return ""
	}
	return parsed.String()
}

func portalTicketPriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "blocking", "urgent":
		return "urgent"
	case "major", "high":
		return "high"
	case "minor", "normal":
		return "normal"
	case "question", "low":
		return "low"
	default:
		return "normal"
	}
}

func secondsUntil(at time.Time) int {
	seconds := int(time.Until(at).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

func writePortalNotFound(w http.ResponseWriter, r *http.Request) {
	httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "That request does not exist.")
}

func writePortalError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, portal.ErrNotFound), errors.Is(err, portal.ErrNotConfigured):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "This portal is not available.")
	case errors.Is(err, portal.ErrPortalRequired):
		httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Choose a portal before continuing.")
	case errors.Is(err, portal.ErrTokenInvalid), errors.Is(err, portal.ErrTokenExpired):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "This sign-in link is invalid or expired.")
	case errors.Is(err, portal.ErrSessionInvalid):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Your portal session has expired.")
	case errors.Is(err, portal.ErrInvalidProfile):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "Check your profile details and try again.")
	case errors.Is(err, portal.ErrCustomerNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Your customer profile is not available.")
	case errors.Is(err, portal.ErrForbidden):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This portal sign-in method is not enabled.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "The portal could not complete that request.")
	}
}
