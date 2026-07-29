package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/widget"
)

// registerWidgetRoutes mounts two distinct surfaces on the same router:
//
//   - the dashboard's authenticated CRUD, gated by WidgetManage like every
//     other settings surface;
//   - the widget's own public, unauthenticated bootstrap and visitor-facing
//     conversation endpoints, gated instead by public-key + origin-allowlist
//     checks that happen inside internal/widget itself (§11.4).
//
// The public routes carry CORS headers by hand rather than through the
// shared middleware chain: they are the one part of /api that must answer a
// browser running on an arbitrary customer domain, and everything else in
// this router deliberately stays same-origin only.
func registerWidgetRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/widgets",
		requireCapability(deps, authorization.WidgetManage, handleListWidgets(deps)))
	mux.HandleFunc("POST /v1/widgets",
		requireCapability(deps, authorization.WidgetManage, handleCreateWidget(deps)))
	mux.HandleFunc("GET /v1/widgets/{id}",
		requireCapability(deps, authorization.WidgetManage, handleGetWidget(deps)))
	mux.HandleFunc("PUT /v1/widgets/{id}",
		requireCapability(deps, authorization.WidgetManage, handleUpdateWidget(deps)))
	mux.HandleFunc("DELETE /v1/widgets/{id}",
		requireCapability(deps, authorization.WidgetManage, handleDeleteWidget(deps)))

	mux.HandleFunc("GET /v1/widgets/{id}/versions",
		requireCapability(deps, authorization.WidgetManage, handleListWidgetVersions(deps)))
	mux.HandleFunc("POST /v1/widgets/{id}/rollback",
		requireCapability(deps, authorization.WidgetManage, handleRollbackWidget(deps)))

	mux.HandleFunc("GET /v1/widgets/{id}/domains",
		requireCapability(deps, authorization.WidgetManage, handleListWidgetDomains(deps)))
	mux.HandleFunc("PUT /v1/widgets/{id}/domains",
		requireCapability(deps, authorization.WidgetManage, handleReplaceWidgetDomains(deps)))
	mux.HandleFunc("POST /v1/widgets/{id}/domains",
		requireCapability(deps, authorization.WidgetManage, handleAddWidgetDomain(deps)))
	mux.HandleFunc("DELETE /v1/widgets/{id}/domains/{domainID}",
		requireCapability(deps, authorization.WidgetManage, handleRemoveWidgetDomain(deps)))

	mux.HandleFunc("GET /v1/widgets/{id}/identity-secret",
		requireCapability(deps, authorization.WidgetManage, handleWidgetIdentitySecret(deps)))

	// -------------------------------------------------------- public surface
	mux.HandleFunc("GET /v1/widget/config", withPublicCORS(handleWidgetConfig(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/config", corsPreflight)

	mux.HandleFunc("POST /v1/widget/visitors", withPublicCORS(handleIssueVisitor(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/visitors", corsPreflight)

	mux.HandleFunc("POST /v1/widget/identify", withPublicCORS(handleWidgetIdentify(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/identify", corsPreflight)

	mux.HandleFunc("POST /v1/widget/events", withPublicCORS(handleWidgetTrack(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/events", corsPreflight)

	mux.HandleFunc("POST /v1/widget/conversations", withPublicCORS(handleWidgetStartConversation(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/conversations", corsPreflight)

	mux.HandleFunc("GET /v1/widget/conversations/{id}/messages", withPublicCORS(handleWidgetListMessages(deps)))
	mux.HandleFunc("POST /v1/widget/conversations/{id}/messages", withPublicCORS(handleWidgetPostMessage(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/conversations/{id}/messages", corsPreflight)
}

/* --------------------------------------------------------------- CORS */

// withPublicCORS reflects the caller's origin on every response from the
// public widget surface. There is no ambient credential to protect here —
// the widget authenticates with a bearer visitor token it sends explicitly,
// never a cookie — so reflecting broadly costs nothing that the token itself
// was not already the real boundary for; the domain-allowlist check inside
// internal/widget is what actually decides whether a request succeeds.
func withPublicCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, r)
		next(w, r)
	}
}

func corsPreflight(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "600")
}

/* ---------------------------------------------------------- dashboard CRUD */

func widgetJSON(w widget.Widget, domains []widget.Domain) map[string]any {
	names := make([]string, len(domains))
	for i, d := range domains {
		names[i] = d.Domain
	}
	return map[string]any{
		"id": w.ID, "workspace_id": w.WorkspaceID, "name": w.Name, "public_key": w.PublicKey,
		"inbox_id": w.InboxID, "modes": w.Modes, "appearance": w.Appearance, "content": w.Content,
		"behavior": w.Behavior, "domains": names, "environment": w.Environment,
		"rollout_percent": w.RolloutPercent, "version": w.Version, "enabled": w.Enabled,
		"installed_at": w.InstalledAt, "last_seen_at": w.LastSeenAt, "updated_at": w.UpdatedAt,
	}
}

func loadWidgetJSON(r *http.Request, deps Deps, workspaceID string, w widget.Widget) map[string]any {
	domains, err := deps.Widget.Domains(r.Context(), workspaceID, w.ID)
	if err != nil {
		domains = []widget.Domain{}
	}
	return widgetJSON(w, domains)
}

func handleListWidgets(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		widgets, err := deps.Widget.List(r.Context(), actor.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load widgets.")
			return
		}
		out := make([]map[string]any, len(widgets))
		for i, item := range widgets {
			out[i] = loadWidgetJSON(r, deps, actor.WorkspaceID, item)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type createWidgetRequest struct {
	Name    string  `json:"name"`
	InboxID *string `json:"inbox_id"`
}

func handleCreateWidget(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req createWidgetRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		created, err := deps.Widget.Create(r.Context(), actor.WorkspaceID, actor.MemberID, req.Name, req.InboxID)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, loadWidgetJSON(r, deps, actor.WorkspaceID, *created))
	}
}

func handleGetWidget(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		found, err := deps.Widget.Get(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadWidgetJSON(r, deps, actor.WorkspaceID, *found))
	}
}

type updateWidgetRequest struct {
	Name           string         `json:"name"`
	InboxID        *string        `json:"inbox_id"`
	Modes          []string       `json:"modes"`
	Appearance     map[string]any `json:"appearance"`
	Content        map[string]any `json:"content"`
	Behavior       map[string]any `json:"behavior"`
	Environment    string         `json:"environment"`
	RolloutPercent int            `json:"rollout_percent"`
	Enabled        bool           `json:"enabled"`
	Domains        []string       `json:"domains"`
	Note           *string        `json:"note"`
}

func handleUpdateWidget(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req updateWidgetRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		updated, err := deps.Widget.Update(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), widget.UpdateInput{
			Name: req.Name, InboxID: req.InboxID, Modes: req.Modes, Appearance: req.Appearance,
			Content: req.Content, Behavior: req.Behavior, Environment: req.Environment,
			RolloutPercent: req.RolloutPercent, Enabled: req.Enabled, Note: req.Note,
		})
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		if req.Domains != nil {
			if err := deps.Widget.ReplaceDomains(r.Context(), actor.WorkspaceID, r.PathValue("id"), req.Domains); err != nil {
				writeWidgetError(w, r, err)
				return
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, loadWidgetJSON(r, deps, actor.WorkspaceID, *updated))
	}
}

func handleDeleteWidget(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Widget.Delete(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id")); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleListWidgetVersions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		versions, err := deps.Widget.Versions(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		out := make([]map[string]any, len(versions))
		for i, v := range versions {
			out[i] = map[string]any{
				"id": v.ID, "version": v.Version, "modes": v.Modes, "appearance": v.Appearance,
				"content": v.Content, "behavior": v.Behavior, "changed_by": v.ChangedBy,
				"note": v.Note, "created_at": v.CreatedAt,
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type rollbackWidgetRequest struct {
	Version int `json:"version"`
}

func handleRollbackWidget(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req rollbackWidgetRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		updated, err := deps.Widget.Rollback(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Version)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, loadWidgetJSON(r, deps, actor.WorkspaceID, *updated))
	}
}

func handleListWidgetDomains(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		domains, err := deps.Widget.Domains(r.Context(), actor.WorkspaceID, r.PathValue("id"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		out := make([]map[string]any, len(domains))
		for i, d := range domains {
			out[i] = map[string]any{"id": d.ID, "domain": d.Domain, "verified_at": d.VerifiedAt, "created_at": d.CreatedAt}
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

type replaceDomainsRequest struct {
	Domains []string `json:"domains"`
}

func handleReplaceWidgetDomains(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req replaceDomainsRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		if err := deps.Widget.ReplaceDomains(r.Context(), actor.WorkspaceID, r.PathValue("id"), req.Domains); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type addDomainRequest struct {
	Domain string `json:"domain"`
}

func handleAddWidgetDomain(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		var req addDomainRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		created, err := deps.Widget.AddDomain(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.Domain)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"id": created.ID, "domain": created.Domain})
	}
}

func handleRemoveWidgetDomain(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if err := deps.Widget.RemoveDomain(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), r.PathValue("domainID")); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleWidgetIdentitySecret(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if _, err := deps.Widget.Get(r.Context(), actor.WorkspaceID, r.PathValue("id")); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"secret": deps.Widget.IdentitySecret(actor.WorkspaceID)})
	}
}

/* ------------------------------------------------------------ public surface */

func publicConfigJSON(c *widget.PublicConfig) map[string]any {
	return map[string]any{
		"enabled": c.Enabled, "online": c.Online, "modes": c.Modes,
		"appearance": c.Appearance, "content": c.Content, "behavior": c.Behavior,
		"articles": c.Articles,
	}
}

func handleWidgetConfig(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		config, err := deps.Widget.ResolveConfig(r.Context(), query.Get("key"), query.Get("url"), r.Header.Get("Origin"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, publicConfigJSON(config))
	}
}

type issueVisitorRequest struct {
	PublicKey string `json:"public_key"`
	URL       string `json:"url"`
}

func handleIssueVisitor(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req issueVisitorRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		widgetRecord, err := deps.Widget.WidgetForOrigin(r.Context(), req.PublicKey, req.URL, r.Header.Get("Origin"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		token, visitor, err := deps.Widget.IssueVisitor(r.Context(), widgetRecord.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not start a session.")
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"token": token, "visitor_id": visitor.ID})
	}
}

// widgetVisitorRequest is the envelope every public POST beyond visitor
// issuance carries: which widget, which page, and which visitor is making
// the call. Embedded (not a separate params struct) so each request type
// below can add its own fields alongside these three.
type widgetVisitorRequest struct {
	PublicKey string `json:"public_key"`
	URL       string `json:"url"`
	Token     string `json:"token"`
}

// resolveVisitorRequest runs the two checks every visitor-facing write
// shares: the calling origin is on this widget's allowlist, and the token
// names a real visitor of that same workspace. Handlers call this first and
// use the returned workspace/visitor for everything after.
func resolveVisitorRequest(r *http.Request, deps Deps, req widgetVisitorRequest) (workspaceID string, visitor *widget.Visitor, err error) {
	widgetRecord, err := deps.Widget.WidgetForOrigin(r.Context(), req.PublicKey, req.URL, r.Header.Get("Origin"))
	if err != nil {
		return "", nil, err
	}
	visitor, err = deps.Widget.ResolveVisitor(r.Context(), widgetRecord.WorkspaceID, req.Token)
	if err != nil {
		return "", nil, err
	}
	return widgetRecord.WorkspaceID, visitor, nil
}

type widgetIdentifyRequest struct {
	widgetVisitorRequest
	Name        *string `json:"name"`
	Email       *string `json:"email"`
	ExternalID  *string `json:"external_id"`
	SignedToken *string `json:"signed_token"`
}

func handleWidgetIdentify(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetIdentifyRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req.widgetVisitorRequest)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		cust, err := deps.Widget.Identify(r.Context(), workspaceID, visitor, widget.IdentifyInput{
			Name: req.Name, Email: req.Email, ExternalID: req.ExternalID, SignedToken: req.SignedToken,
		})
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"customer": map[string]any{"id": cust.ID, "name": cust.Name, "email": cust.Email},
		})
	}
}

type widgetTrackRequest struct {
	widgetVisitorRequest
	Type    string         `json:"type"`
	PageURL *string        `json:"page_url"`
	Payload map[string]any `json:"payload"`
}

func handleWidgetTrack(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetTrackRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req.widgetVisitorRequest)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		if _, err := deps.Widget.Track(r.Context(), workspaceID, visitor, req.Type, req.PageURL, req.Payload); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type widgetStartConversationRequest struct {
	widgetVisitorRequest
	Body string `json:"body"`
}

func widgetMessageJSON(m conversation.Message) map[string]any {
	return map[string]any{
		"id": m.ID, "kind": m.Kind, "author_type": m.AuthorType, "author_name": m.AuthorName,
		"body": m.Body, "sequence": m.Sequence, "created_at": m.CreatedAt,
	}
}

func handleWidgetStartConversation(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetStartConversationRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}
		widgetRecord, err := deps.Widget.WidgetForOrigin(r.Context(), req.PublicKey, req.URL, r.Header.Get("Origin"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}

		// A visitor token is minted transparently on the first message if the
		// browser did not already have one — the widget calls "visitors" up
		// front in the common case, but nothing here requires that ordering.
		var visitor *widget.Visitor
		if req.Token != "" {
			visitor, err = deps.Widget.ResolveVisitor(r.Context(), widgetRecord.WorkspaceID, req.Token)
		}
		var issuedToken string
		if req.Token == "" || err != nil {
			issuedToken, visitor, err = deps.Widget.IssueVisitor(r.Context(), widgetRecord.WorkspaceID)
		}
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}

		conv, msg, err := deps.Widget.StartConversation(r.Context(), widgetRecord.WorkspaceID, widgetRecord, visitor, req.Body)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{
			"conversation_id": conv.ID, "token": issuedToken, "message": widgetMessageJSON(*msg),
		})
	}
}

type widgetPostMessageRequest struct {
	widgetVisitorRequest
	Body string `json:"body"`
}

func handleWidgetPostMessage(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetPostMessageRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req.widgetVisitorRequest)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		msg, err := deps.Widget.PostMessage(r.Context(), workspaceID, r.PathValue("id"), visitor, req.Body)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, widgetMessageJSON(*msg))
	}
}

func handleWidgetListMessages(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		req := widgetVisitorRequest{PublicKey: query.Get("key"), URL: query.Get("url"), Token: query.Get("token")}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		after := int64(0)
		if raw := query.Get("after"); raw != "" {
			if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
				after = parsed
			}
		}
		messages, err := deps.Widget.Messages(r.Context(), workspaceID, r.PathValue("id"), visitor, after)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		out := make([]map[string]any, len(messages))
		for i, m := range messages {
			out[i] = widgetMessageJSON(m)
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

func writeWidgetError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, widget.ErrNotFound), errors.Is(err, conversation.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Not found.")
	case errors.Is(err, widget.ErrOriginNotAllowed):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This origin is not allowed to use this widget.")
	case errors.Is(err, widget.ErrVisitorInvalid), errors.Is(err, widget.ErrIdentityTokenInvalid), errors.Is(err, widget.ErrIdentityTokenExpired):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Your session has expired.")
	case errors.Is(err, widget.ErrConversationOwner):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This conversation does not belong to you.")
	case errors.Is(err, widget.ErrDisabled):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This widget is currently disabled.")
	case errors.Is(err, widget.ErrDuplicateDomain):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, widget.ErrInvalidInbox), errors.Is(err, widget.ErrInvalidName),
		errors.Is(err, widget.ErrWildcardDomain), errors.Is(err, widget.ErrNoInbox),
		errors.Is(err, conversation.ErrEmptyBody):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}
