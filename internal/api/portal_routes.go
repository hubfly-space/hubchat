package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/mailer"
	"github.com/hubchat/hubchat/internal/portal"
	"github.com/hubchat/hubchat/internal/ticket"
)

func registerPortalRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/portal/bootstrap", handlePortalBootstrap(deps))
	mux.HandleFunc("POST /v1/portal/auth/magic-link", handlePortalMagicLink(deps))
	mux.HandleFunc("POST /v1/portal/auth/magic-link/redeem", handlePortalMagicLinkRedeem(deps))
	mux.HandleFunc("POST /v1/portal/auth/logout", handlePortalLogout(deps))
	mux.HandleFunc("GET /v1/portal/me", handlePortalMe(deps))
	mux.HandleFunc("GET /v1/portal/tickets", handlePortalTickets(deps))
	mux.HandleFunc("POST /v1/portal/tickets", Idempotency(deps)(handlePortalCreateTicket(deps)))
	mux.HandleFunc("GET /v1/portal/tickets/{id}", handlePortalTicket(deps))
	mux.HandleFunc("POST /v1/portal/tickets/{id}/replies", handlePortalTicketReply(deps))
}

type portalBootstrapResponse struct {
	Portal  map[string]any     `json:"portal"`
	Viewer  *portal.Customer   `json:"viewer"`
	Session *portalSessionJSON `json:"session,omitempty"`
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
				response.Viewer = &session.Customer
				response.Session = &portalSessionJSON{
					PortalID: session.PortalID, ExpiresAt: session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
					AuthMethod: session.AuthMethod,
				}
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, response)
	}
}

type portalMagicLinkRequest struct {
	Portal string `json:"portal"`
	Email  string `json:"email"`
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
				deps.sendMail(r, link.Customer.Email, "Your Hubchat portal sign-in link", "magic_link", mailer.Data{
					Name: link.Customer.Name, Link: portalMagicLink(deps, p.ID, link.Token), ExpiresIn: "15 minutes",
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
		httpserver.WriteJSON(w, http.StatusOK, portalBootstrapResponse{
			Portal: portalJSON(*session.Portal), Viewer: &session.Customer,
			Session: &portalSessionJSON{PortalID: session.PortalID, ExpiresAt: session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"), AuthMethod: session.AuthMethod},
		})
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
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"viewer": session.Customer, "portal": portalJSON(*session.Portal)})
	}
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
		conv, _, err := deps.Conversation.Start(r.Context(), session.WorkspaceID, *session.Portal.DefaultInboxID, "portal", &conversationSubject, &customerID, nil, session.Customer.Name, conversationBody)
		if err != nil {
			if errors.Is(err, conversation.ErrEmptyBody) {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationError, "Describe what you need help with.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not start your request.")
			return
		}
		ticket, err := deps.Ticket.CreateAsCustomer(r.Context(), session.WorkspaceID, customerID, session.Customer.Name, ticket.CreateRequest{
			Title: req.Title, Description: req.Description, Priority: "normal", CustomerID: &customerID,
			InboxID: *session.Portal.DefaultInboxID, Channel: "portal", ConversationID: &conv.ID,
		})
		if err != nil {
			deps.Logger.Error("creating portal ticket failed after conversation creation", "error", err, "conversation_id", conv.ID)
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not create your request.")
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"ticket": portalTicketJSON(*ticket)})
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
		out := map[string]any{"ticket": portalTicketJSON(*ticket)}
		messages := []map[string]any{}
		if ticket.ConversationID != nil {
			all, messageErr := deps.Conversation.Messages(r.Context(), session.WorkspaceID, *ticket.ConversationID, 0)
			if messageErr != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load this request.")
				return
			}
			for _, message := range all {
				if message.Kind == "reply" {
					messages = append(messages, portalMessageJSON(message))
				}
			}
		}
		out["messages"] = messages
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
	return map[string]any{
		"id": m.ID, "conversation_id": m.ConversationID, "kind": m.Kind,
		"author_type": m.AuthorType, "author_id": m.AuthorID, "author_name": m.AuthorName,
		"body": m.Body, "delivery": m.Delivery, "sequence": m.Sequence,
		"edited_at": m.EditedAt, "created_at": m.CreatedAt,
	}
}

type portalReplyRequest struct {
	Body     string  `json:"body"`
	ClientID *string `json:"client_id"`
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
		httpserver.WriteJSON(w, http.StatusCreated, portalMessageJSON(*message))
	}
}

func requirePortalSession(deps Deps, w http.ResponseWriter, r *http.Request) (*portal.Session, bool) {
	token := httpserver.PortalSessionToken(r)
	if token == "" {
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Sign in to continue.")
		return nil, false
	}
	session, err := deps.Portal.Session(r.Context(), token, portalIdentifier(r))
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
	return deps.Portal.Resolve(r.Context(), strings.TrimSpace(identifier))
}

func portalIdentifier(r *http.Request) string {
	if value := r.URL.Query().Get("portal"); value != "" {
		return value
	}
	return r.Header.Get("Hubchat-Portal-Id")
}

func portalJSON(p portal.Portal) map[string]any {
	return map[string]any{
		"id": p.ID, "workspace_id": p.WorkspaceID, "name": p.Name, "subdomain": p.Subdomain,
		"theme": p.Theme, "features": p.Features, "auth_methods": p.AuthMethods,
		"permissions": p.Permissions, "default_language": p.DefaultLanguage,
		"navigation": p.Navigation, "enabled": p.Enabled,
	}
}

func portalMagicLink(deps Deps, portalID, token string) string {
	if deps.PublicURL == nil {
		return fmt.Sprintf("/portal/sign-in?portal=%s&token=%s", url.QueryEscape(portalID), url.QueryEscape(token))
	}
	target := *deps.PublicURL
	target.Path = strings.TrimSuffix(target.Path, "/") + "/portal/sign-in"
	query := target.Query()
	query.Set("portal", portalID)
	query.Set("token", token)
	target.RawQuery = query.Encode()
	return target.String()
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
	case errors.Is(err, portal.ErrForbidden):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This portal sign-in method is not enabled.")
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "The portal could not complete that request.")
	}
}
