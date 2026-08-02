package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/feedback"
	"github.com/hubchat/hubchat/internal/file"
	formmodule "github.com/hubchat/hubchat/internal/form"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/knowledgebase"
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
	idempotent := Idempotency(deps)
	widgetIdempotent := WidgetIdempotency(deps)
	mux.HandleFunc("GET /v1/widgets",
		requireCapability(deps, authorization.WidgetManage, handleListWidgets(deps)))
	mux.HandleFunc("POST /v1/widgets",
		requireCapability(deps, authorization.WidgetManage, idempotent(handleCreateWidget(deps))))
	mux.HandleFunc("GET /v1/widgets/{id}",
		requireCapability(deps, authorization.WidgetManage, handleGetWidget(deps)))
	mux.HandleFunc("PUT /v1/widgets/{id}",
		requireCapability(deps, authorization.WidgetManage, idempotent(handleUpdateWidget(deps))))
	mux.HandleFunc("DELETE /v1/widgets/{id}",
		requireCapability(deps, authorization.WidgetManage, idempotent(handleDeleteWidget(deps))))

	mux.HandleFunc("GET /v1/widgets/{id}/versions",
		requireCapability(deps, authorization.WidgetManage, handleListWidgetVersions(deps)))
	mux.HandleFunc("POST /v1/widgets/{id}/rollback",
		requireCapability(deps, authorization.WidgetManage, idempotent(handleRollbackWidget(deps))))

	mux.HandleFunc("GET /v1/widgets/{id}/domains",
		requireCapability(deps, authorization.WidgetManage, handleListWidgetDomains(deps)))
	mux.HandleFunc("PUT /v1/widgets/{id}/domains",
		requireCapability(deps, authorization.WidgetManage, idempotent(handleReplaceWidgetDomains(deps))))
	mux.HandleFunc("POST /v1/widgets/{id}/domains",
		requireCapability(deps, authorization.WidgetManage, idempotent(handleAddWidgetDomain(deps))))
	mux.HandleFunc("DELETE /v1/widgets/{id}/domains/{domainID}",
		requireCapability(deps, authorization.WidgetManage, idempotent(handleRemoveWidgetDomain(deps))))

	mux.HandleFunc("GET /v1/widgets/{id}/identity-secret",
		requireCapability(deps, authorization.WidgetManage, handleWidgetIdentitySecret(deps)))
	mux.HandleFunc("GET /v1/customer-command-bindings",
		requireCapability(deps, authorization.IntegrationManage, handleListCommandBindings(deps)))
	mux.HandleFunc("POST /v1/customer-command-bindings",
		requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleCreateCommandBinding(deps))))
	mux.HandleFunc("PATCH /v1/customer-command-bindings/{id}",
		requireCapability(deps, authorization.IntegrationManage, Idempotency(deps)(handleUpdateCommandBinding(deps))))
	mux.HandleFunc("POST /v1/conversations/{id}/customer-commands",
		requireCapability(deps, authorization.ConversationReply, Idempotency(deps)(handleInvokeCustomerCommand(deps))))

	// -------------------------------------------------------- public surface
	mux.HandleFunc("GET /v1/widget/config", withPublicCORS(handleWidgetConfig(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/config", corsPreflight)

	mux.HandleFunc("POST /v1/widget/visitors", withPublicCORS(widgetIdempotent(handleIssueVisitor(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/visitors", corsPreflight)

	mux.HandleFunc("POST /v1/widget/identify", withPublicCORS(widgetIdempotent(handleWidgetIdentify(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/identify", corsPreflight)

	mux.HandleFunc("POST /v1/widget/events", withPublicCORS(widgetIdempotent(handleWidgetTrack(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/events", corsPreflight)
	mux.HandleFunc("POST /v1/widget/conversations/{id}/commands/{commandID}/ack", withPublicCORS(widgetIdempotent(handleWidgetCommandAck(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/conversations/{id}/commands/{commandID}/ack", corsPreflight)
	mux.HandleFunc("GET /v1/widget/forms", withPublicCORS(handleWidgetListForms(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/forms", corsPreflight)
	mux.HandleFunc("GET /v1/widget/forms/{slug}", withPublicCORS(handleWidgetGetForm(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/forms/{slug}", corsPreflight)
	mux.HandleFunc("POST /v1/widget/forms/{slug}/files", withPublicCORS(widgetIdempotent(handleWidgetFormFileUpload(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/forms/{slug}/files", corsPreflight)
	mux.HandleFunc("POST /v1/widget/forms/{slug}/submissions", withPublicCORS(widgetIdempotent(handleWidgetSubmitForm(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/forms/{slug}/submissions", corsPreflight)
	mux.HandleFunc("GET /v1/widget/feedback/boards", withPublicCORS(handleWidgetFeedbackBoards(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/feedback/boards", corsPreflight)
	mux.HandleFunc("GET /v1/widget/feedback/boards/{slug}/items", withPublicCORS(handleWidgetFeedbackItems(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/feedback/boards/{slug}/items", corsPreflight)
	mux.HandleFunc("POST /v1/widget/feedback/boards/{slug}/items", withPublicCORS(widgetIdempotent(handleWidgetFeedbackCreate(deps))))
	mux.HandleFunc("POST /v1/widget/feedback/items/{id}/votes", withPublicCORS(widgetIdempotent(handleWidgetFeedbackVote(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/feedback/items/{id}/votes", corsPreflight)
	mux.HandleFunc("POST /v1/widget/feedback/items/{id}/subscription", withPublicCORS(widgetIdempotent(handleWidgetFeedbackSubscription(deps))))
	mux.HandleFunc("DELETE /v1/widget/feedback/items/{id}/subscription", withPublicCORS(widgetIdempotent(handleWidgetFeedbackSubscription(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/feedback/items/{id}/subscription", corsPreflight)
	mux.HandleFunc("GET /v1/widget/articles", withPublicCORS(handleWidgetArticleSearch(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/articles", corsPreflight)
	mux.HandleFunc("GET /v1/widget/articles/{slug}", withPublicCORS(handleWidgetArticle(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/articles/{slug}", corsPreflight)
	mux.HandleFunc("POST /v1/widget/articles/{slug}/feedback", withPublicCORS(widgetIdempotent(handleWidgetArticleFeedback(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/articles/{slug}/feedback", corsPreflight)

	mux.HandleFunc("POST /v1/widget/conversations", withPublicCORS(widgetIdempotent(handleWidgetStartConversation(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/conversations", corsPreflight)

	mux.HandleFunc("GET /v1/widget/conversations/{id}/messages", withPublicCORS(handleWidgetListMessages(deps)))
	mux.HandleFunc("POST /v1/widget/conversations/{id}/messages", withPublicCORS(widgetIdempotent(handleWidgetPostMessage(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/conversations/{id}/messages", corsPreflight)
	mux.HandleFunc("GET /v1/widget/conversations/{id}/commands", withPublicCORS(handleWidgetPendingCommands(deps)))
	mux.HandleFunc("OPTIONS /v1/widget/conversations/{id}/commands", corsPreflight)
	mux.HandleFunc("POST /v1/widget/conversations/{id}/files", withPublicCORS(widgetIdempotent(handleWidgetFileUpload(deps))))
	mux.HandleFunc("OPTIONS /v1/widget/conversations/{id}/files", corsPreflight)
	mux.HandleFunc("GET /v1/widget/conversations/{id}/files/{fileID}", withPublicCORS(handleWidgetFileDownload(deps)))
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
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed widget cursor.")
			return
		}
		widgets, err := deps.Widget.ListPage(r.Context(), actor.WorkspaceID, cursor.At, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load widgets.")
			return
		}
		page := NewPage(widgets, limit, func(item widget.Widget) Cursor { return Cursor{At: item.CreatedAt, ID: item.ID} })
		out := make([]map[string]any, 0, len(page.Data))
		for _, item := range page.Data {
			out = append(out, loadWidgetJSON(r, deps, actor.WorkspaceID, item))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
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
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		beforeVersion := 0
		if cursor.Value != "" {
			beforeVersion, err = strconv.Atoi(cursor.Value)
			if err != nil || beforeVersion <= 0 {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
				return
			}
		}
		versions, err := deps.Widget.VersionsPage(r.Context(), actor.WorkspaceID, r.PathValue("id"), beforeVersion, limit+1)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		page := NewPage(versions, limit, func(v widget.ConfigVersion) Cursor {
			return Cursor{Value: strconv.Itoa(v.Version), ID: v.ID}
		})
		out := make([]map[string]any, len(page.Data))
		for i, v := range page.Data {
			out[i] = map[string]any{
				"id": v.ID, "version": v.Version, "modes": v.Modes, "appearance": v.Appearance,
				"content": v.Content, "behavior": v.Behavior, "changed_by": v.ChangedBy,
				"note": v.Note, "created_at": v.CreatedAt,
			}
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
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
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed widget domain cursor.")
			return
		}
		domains, err := deps.Widget.DomainsPage(r.Context(), actor.WorkspaceID, r.PathValue("id"), cursor.At, cursor.ID, limit+1)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		out := make([]map[string]any, len(domains))
		for i, d := range domains {
			out[i] = map[string]any{"id": d.ID, "domain": d.Domain, "verified_at": d.VerifiedAt, "created_at": d.CreatedAt}
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(out, limit, func(item map[string]any) Cursor {
			return Cursor{At: item["created_at"].(time.Time), ID: item["id"].(string)}
		}))
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
		"enabled": c.Enabled, "online": c.Online, "language": c.Language, "modes": c.Modes,
		"appearance": c.Appearance, "content": c.Content, "behavior": c.Behavior,
		"articles": c.Articles,
	}
}

func handleWidgetConfig(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		config, err := deps.Widget.ResolveConfigForLanguage(r.Context(), query.Get("key"), query.Get("url"), r.Header.Get("Origin"), query.Get("language"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		if config.Enabled && containsString(config.Modes, "knowledge_base") && deps.Knowledgebase != nil {
			widgetRecord, lookupErr := deps.Widget.WidgetForOrigin(r.Context(), query.Get("key"), query.Get("url"), r.Header.Get("Origin"))
			if lookupErr != nil {
				writeWidgetError(w, r, lookupErr)
				return
			}
			articles, searchErr := deps.Knowledgebase.ListPublished(r.Context(), widgetRecord.WorkspaceID, r.URL.Query().Get("language"), 8)
			if searchErr != nil {
				writeKnowledgebaseInternal(w, r)
				return
			}
			config.Articles = widgetArticleSummaries(articles)
		}
		httpserver.WriteJSON(w, http.StatusOK, publicConfigJSON(config))
	}
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func widgetArticleSummaries(items []knowledgebase.Article) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{"slug": item.Slug, "title": item.Title, "excerpt": item.Excerpt})
	}
	return result
}

func widgetArticleJSON(item *knowledgebase.Article, includeBody bool) map[string]any {
	result := map[string]any{"slug": item.Slug, "title": item.Title, "excerpt": item.Excerpt, "language": item.Language, "helpful_count": item.HelpfulCount, "unhelpful_count": item.UnhelpfulCount}
	if includeBody {
		result["body"] = item.Body
	}
	return result
}

func handleWidgetArticleSearch(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := widgetPublicRecord(deps, r)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		if deps.Knowledgebase == nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed article cursor.")
			return
		}
		// Keep the widget response small even when a caller asks for the
		// platform-wide maximum page size. The pre-fetched bootstrap list is
		// intentionally tiny; this endpoint is the bounded continuation path.
		pageLimit := limit
		if pageLimit > 20 {
			pageLimit = 20
		}
		var beforeRank *float32
		if !cursor.IsZero() {
			value, parseErr := strconv.ParseFloat(cursor.Value, 32)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed article cursor.")
				return
			}
			rank := float32(value)
			beforeRank = &rank
		}
		items, err := deps.Knowledgebase.SearchPublishedPage(r.Context(), item.WorkspaceID, "", "", r.URL.Query().Get("q"), r.URL.Query().Get("language"), beforeRank, cursor.At, cursor.ID, pageLimit+1)
		if err != nil {
			writeKnowledgebaseInternal(w, r)
			return
		}
		page := NewPage(items, pageLimit, func(searchResult knowledgebase.SearchResult) Cursor {
			at := time.Time{}
			if searchResult.Article.PublishedAt != nil {
				at = *searchResult.Article.PublishedAt
			}
			return Cursor{Value: strconv.FormatFloat(float64(searchResult.Rank), 'g', -1, 32), At: at, ID: searchResult.Article.ID}
		})
		result := make([]map[string]any, 0, len(page.Data))
		for _, searchResult := range page.Data {
			result = append(result, widgetArticleJSON(&searchResult.Article, false))
		}
		if cursor.IsZero() {
			deps.Knowledgebase.RecordSearch(r.Context(), item.WorkspaceID, r.URL.Query().Get("q"), r.URL.Query().Get("language"), "widget", len(page.Data))
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: result, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleWidgetArticle(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := widgetPublicRecord(deps, r)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		article, err := deps.Knowledgebase.GetPublishedBySlugSurfaceLanguage(r.Context(), item.WorkspaceID, r.PathValue("slug"), "widget", r.URL.Query().Get("language"))
		if err != nil {
			if errors.Is(err, knowledgebase.ErrNotFound) {
				httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Article not found.")
				return
			}
			writeKnowledgebaseInternal(w, r)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, widgetArticleJSON(article, true))
	}
}

func handleWidgetArticleFeedback(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := widgetPublicRecord(deps, r)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		var input struct {
			Helpful  bool   `json:"helpful"`
			Comment  string `json:"comment"`
			Language string `json:"language"`
		}
		if err := httpserver.DecodeJSON(r, &input); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed article feedback.")
			return
		}
		language := strings.TrimSpace(r.URL.Query().Get("language"))
		if language == "" {
			language = input.Language
		}
		article, err := deps.Knowledgebase.GetPublishedBySlugSurfaceLanguage(r.Context(), item.WorkspaceID, r.PathValue("slug"), "widget", language)
		if err != nil {
			writeKnowledgebaseError(w, r, err)
			return
		}
		fingerprint := sha256.Sum256([]byte(clientIP(r) + "|" + r.UserAgent()))
		if err := deps.Knowledgebase.RecordArticleFeedback(r.Context(), item.WorkspaceID, article.ID, input.Helpful, input.Comment, "", fingerprint[:]); err != nil {
			if errors.Is(err, knowledgebase.ErrFeedbackAlreadyRecorded) {
				httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "Feedback was already recorded for this article.")
				return
			}
			writeKnowledgebaseError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"status": "recorded"})
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
	Name        *string        `json:"name"`
	Email       *string        `json:"email"`
	ExternalID  *string        `json:"external_id"`
	SignedToken *string        `json:"signed_token"`
	Attributes  map[string]any `json:"attributes"`
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
			Name: req.Name, Email: req.Email, ExternalID: req.ExternalID, SignedToken: req.SignedToken, Attributes: req.Attributes,
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

func widgetFormRequest(r *http.Request) widgetVisitorRequest {
	return widgetVisitorRequest{PublicKey: r.URL.Query().Get("key"), URL: r.URL.Query().Get("url"), Token: r.URL.Query().Get("token")}
}

func handleWidgetListForms(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		widgetRecord, err := deps.Widget.WidgetForOrigin(r.Context(), r.URL.Query().Get("key"), r.URL.Query().Get("url"), r.Header.Get("Origin"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		limit, cursor, pageErr := PageParams(r)
		if pageErr != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed form cursor.")
			return
		}
		items, err := deps.Form.ListPublicPage(r.Context(), widgetRecord.WorkspaceID, cursor.Value, cursor.ID, limit+1)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, formJSON(item, false))
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(out, limit, func(item map[string]any) Cursor { return Cursor{Value: item["name"].(string), ID: item["id"].(string)} }))
	}
}

func handleWidgetGetForm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		widgetRecord, err := deps.Widget.WidgetForOrigin(r.Context(), r.URL.Query().Get("key"), r.URL.Query().Get("url"), r.Header.Get("Origin"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		item, err := deps.Form.GetPublic(r.Context(), widgetRecord.WorkspaceID, r.PathValue("slug"))
		if err != nil {
			writeWidgetError(w, r, formmodule.ErrNotFound)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, formJSON(*item, false))
	}
}

type widgetFormSubmissionRequest struct {
	widgetVisitorRequest
	Values  map[string]any    `json:"values"`
	FileIDs map[string]string `json:"file_ids,omitempty"`
}

func handleWidgetSubmitForm(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetFormSubmissionRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed form submission.")
			return
		}
		widgetRecord, err := deps.Widget.WidgetForOrigin(r.Context(), req.PublicKey, req.URL, r.Header.Get("Origin"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		var visitor *widget.Visitor
		var issuedToken string
		if strings.TrimSpace(req.Token) != "" {
			visitor, err = deps.Widget.ResolveVisitor(r.Context(), widgetRecord.WorkspaceID, req.Token)
		}
		if visitor == nil || err != nil {
			issuedToken, visitor, err = deps.Widget.IssueVisitor(r.Context(), widgetRecord.WorkspaceID)
		}
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		input := formmodule.SubmissionInput{Values: req.Values, FileIDs: req.FileIDs, VisitorID: visitor.ID, SourceURL: req.URL, IP: clientIP(r), UserAgent: r.UserAgent()}
		if visitor.CustomerID != nil {
			input.CustomerID = *visitor.CustomerID
		}
		id, err := deps.Form.Submit(r.Context(), widgetRecord.WorkspaceID, r.PathValue("slug"), input)
		if err != nil {
			writeFormError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "accepted", "token": issuedToken})
	}
}

func handleWidgetFormFileUpload(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := widgetVisitorRequest{PublicKey: r.FormValue("public_key"), URL: r.FormValue("url"), Token: r.FormValue("token")}
		widgetRecord, err := deps.Widget.WidgetForOrigin(r.Context(), req.PublicKey, req.URL, r.Header.Get("Origin"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		workspaceID := widgetRecord.WorkspaceID
		var visitor *widget.Visitor
		issuedToken := req.Token
		if req.Token == "" {
			issuedToken, visitor, err = deps.Widget.IssueVisitor(r.Context(), workspaceID)
		} else {
			visitor, err = deps.Widget.ResolveVisitor(r.Context(), workspaceID, req.Token)
		}
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		if _, err := deps.Form.GetPublic(r.Context(), workspaceID, r.PathValue("slug")); err != nil {
			writeWidgetError(w, r, formmodule.ErrNotFound)
			return
		}
		if deps.File == nil {
			writeWidgetError(w, r, errors.New("file service unavailable"))
			return
		}
		created, err := uploadFormFile(r, deps.File, workspaceID, "visitor", visitor.ID)
		if err != nil {
			writeFileError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{
			"id": created.ID, "name": created.Name, "mime_type": created.MIMEType,
			"size_bytes": created.SizeBytes, "token": issuedToken,
		})
	}
}

func widgetPublicRecord(deps Deps, r *http.Request) (*widget.Widget, error) {
	return deps.Widget.WidgetForOrigin(r.Context(), r.URL.Query().Get("key"), r.URL.Query().Get("url"), r.Header.Get("Origin"))
}

func handleWidgetFeedbackBoards(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := widgetPublicRecord(deps, r)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		limit, cursor, pageErr := PageParams(r)
		if pageErr != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
			return
		}
		var beforePosition *int
		if !cursor.IsZero() {
			position, parseErr := strconv.Atoi(cursor.Value)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
				return
			}
			beforePosition = &position
		}
		boards, err := deps.Feedback.ListPublicBoardsPage(r.Context(), item.WorkspaceID, beforePosition, cursor.ID, limit+1)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(boards, limit, func(board feedback.Board) Cursor {
			return Cursor{Value: strconv.Itoa(board.Position), ID: board.ID}
		}))
	}
}

func handleWidgetFeedbackItems(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := widgetPublicRecord(deps, r)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		customerID := ""
		if token := r.URL.Query().Get("token"); token != "" {
			if visitor, resolveErr := deps.Widget.ResolveVisitor(r.Context(), item.WorkspaceID, token); resolveErr == nil && visitor.CustomerID != nil {
				customerID = *visitor.CustomerID
			}
		}
		board, err := deps.Feedback.GetBoard(r.Context(), item.WorkspaceID, r.PathValue("slug"), true)
		if err != nil {
			writeWidgetError(w, r, feedback.ErrNotFound)
			return
		}
		limit, cursor, pageErr := PageParams(r)
		if pageErr != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
			return
		}
		sortOrder := r.URL.Query().Get("sort")
		var beforeVote *int64
		if sortOrder != "recent" && !cursor.IsZero() {
			value, parseErr := strconv.ParseInt(cursor.Value, 10, 64)
			if parseErr != nil {
				httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback cursor.")
				return
			}
			beforeVote = &value
		}
		items, err := deps.Feedback.ListItemsPage(r.Context(), item.WorkspaceID, board.ID, r.URL.Query().Get("status"), sortOrder, r.URL.Query().Get("q"), customerID, cursor.At, cursor.ID, beforeVote, limit+1)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		page := NewPage(items, limit, func(item feedback.Item) Cursor {
			value := ""
			if sortOrder != "recent" {
				value = strconv.Itoa(item.VoteCount)
			}
			return Cursor{Value: value, At: item.CreatedAt, ID: item.ID}
		})
		httpserver.WriteJSON(w, http.StatusOK, page)
	}
}

type widgetFeedbackCreateRequest struct {
	widgetVisitorRequest
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

func handleWidgetFeedbackCreate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetFeedbackCreateRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed feedback submission.")
			return
		}
		item, err := deps.Widget.WidgetForOrigin(r.Context(), req.PublicKey, req.URL, r.Header.Get("Origin"))
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		visitor, err := deps.Widget.ResolveVisitor(r.Context(), item.WorkspaceID, req.Token)
		var issuedToken string
		if err != nil || visitor == nil {
			issuedToken, visitor, err = deps.Widget.IssueVisitor(r.Context(), item.WorkspaceID)
		}
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		board, err := deps.Feedback.GetBoard(r.Context(), item.WorkspaceID, r.PathValue("slug"), true)
		if err != nil {
			writeWidgetError(w, r, feedback.ErrNotFound)
			return
		}
		customerID := ""
		if visitor.CustomerID != nil {
			customerID = *visitor.CustomerID
		}
		created, err := deps.Feedback.CreateItem(r.Context(), item.WorkspaceID, board.ID, "", feedback.ItemInput{Title: req.Title, Description: req.Description, Type: req.Type}, customerID)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"item": created, "token": issuedToken})
	}
}

type widgetFeedbackVoteRequest struct{ widgetVisitorRequest }

func handleWidgetFeedbackVote(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetFeedbackVoteRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed vote request.")
			return
		}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req.widgetVisitorRequest)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		if visitor.CustomerID == nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Identify yourself before voting.")
			return
		}
		if err := deps.Feedback.Vote(r.Context(), workspaceID, r.PathValue("id"), *visitor.CustomerID); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{"status": "voted"})
	}
}

func handleWidgetFeedbackSubscription(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetVisitorRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed subscription request.")
			return
		}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		if visitor.CustomerID == nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Identify yourself before following feedback.")
			return
		}
		if r.Method == http.MethodDelete {
			err = deps.Feedback.Unsubscribe(r.Context(), workspaceID, r.PathValue("id"), *visitor.CustomerID)
		} else {
			err = deps.Feedback.Subscribe(r.Context(), workspaceID, r.PathValue("id"), *visitor.CustomerID)
		}
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"subscribed": r.Method != http.MethodDelete})
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

func widgetMessageJSONWithAttachments(r *http.Request, deps Deps, workspaceID string, m conversation.Message) map[string]any {
	out := widgetMessageJSON(m)
	attachments := []map[string]any{}
	if deps.File != nil {
		if records, err := deps.File.MessageAttachments(r.Context(), workspaceID, m.ID); err == nil {
			for _, record := range records {
				attachments = append(attachments, map[string]any{
					"id": record.ID, "name": record.Name, "mime_type": record.MIMEType,
					"size_bytes": record.SizeBytes,
					"url":        "/api/v1/widget/conversations/" + m.ConversationID + "/files/" + record.ID,
				})
			}
		}
	}
	out["attachments"] = attachments
	return out
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
			"conversation_id": conv.ID, "token": issuedToken, "message": widgetMessageJSONWithAttachments(r, deps, widgetRecord.WorkspaceID, *msg),
		})
	}
}

type widgetPostMessageRequest struct {
	widgetVisitorRequest
	Body    string   `json:"body"`
	FileIDs []string `json:"file_ids"`
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
		if deps.File != nil && len(req.FileIDs) > 0 {
			if !widgetMessageBelongsToConversation(r, deps, workspaceID, msg.ID, r.PathValue("id")) {
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "The message attachment target is invalid.")
				return
			}
			if err := deps.File.AttachToMessage(r.Context(), workspaceID, msg.ID, req.FileIDs); err != nil {
				httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "One or more attachments are not available.")
				return
			}
		}
		httpserver.WriteJSON(w, http.StatusCreated, widgetMessageJSONWithAttachments(r, deps, workspaceID, *msg))
	}
}

func handleWidgetFileUpload(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "The upload could not be read.")
			return
		}
		req := widgetVisitorRequest{PublicKey: r.FormValue("public_key"), URL: r.FormValue("url"), Token: r.FormValue("token")}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		conversationID := r.PathValue("id")
		if _, err := deps.Widget.Conversation(r.Context(), workspaceID, conversationID, visitor); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		messageID := strings.TrimSpace(r.FormValue("message_id"))
		if messageID == "" {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeValidationError, "The message attachment target is required.")
			return
		}
		if !widgetMessageBelongsToConversation(r, deps, workspaceID, messageID, conversationID) {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "The message attachment target is invalid.")
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
		created, err := deps.File.Create(r.Context(), workspaceID, file.UploadInput{Name: filepath.Base(part.Filename), MIMEType: mimeType, SizeBytes: part.Size, Body: opened, OwnerType: "conversation", OwnerID: conversationID, UploadedByType: "visitor", UploadedByID: visitor.ID})
		if err != nil {
			writeFileError(w, r, err)
			return
		}
		if err := deps.File.AttachToMessage(r.Context(), workspaceID, messageID, []string{created.ID}); err != nil {
			_ = deps.File.Delete(r.Context(), workspaceID, created.ID)
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "The attachment could not be linked.")
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{
			"id": created.ID, "name": created.Name, "mime_type": created.MIMEType,
			"size_bytes": created.SizeBytes,
			"url":        "/api/v1/widget/conversations/" + conversationID + "/files/" + created.ID,
		})
	}
}

func widgetMessageBelongsToConversation(r *http.Request, deps Deps, workspaceID, messageID, conversationID string) bool {
	var belongs bool
	return deps.Pool.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1 FROM messages
			WHERE workspace_id=$1 AND id=$2 AND conversation_id=$3
		)
	`, workspaceID, messageID, conversationID).Scan(&belongs) == nil && belongs
}

func handleWidgetFileDownload(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		req := widgetVisitorRequest{PublicKey: query.Get("key"), URL: query.Get("url"), Token: query.Get("token")}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		conversationID := r.PathValue("id")
		if _, err := deps.Widget.Conversation(r.Context(), workspaceID, conversationID, visitor); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		fileID := r.PathValue("fileID")
		var attached bool
		if err := deps.Pool.QueryRow(r.Context(), `
			SELECT EXISTS(
				SELECT 1 FROM message_attachments ma
				JOIN messages m ON m.id=ma.message_id AND m.workspace_id=$1 AND m.conversation_id=$2
				WHERE ma.file_id=$3
			)
		`, workspaceID, conversationID, fileID).Scan(&attached); err != nil || !attached {
			writeWidgetError(w, r, conversation.ErrNotFound)
			return
		}
		record, opened, err := deps.File.Open(r.Context(), workspaceID, fileID)
		if err != nil || record.OwnerType != "conversation" || record.OwnerID != conversationID {
			if opened != nil {
				_ = opened.Close()
			}
			writeWidgetError(w, r, conversation.ErrNotFound)
			return
		}
		defer opened.Close()
		w.Header().Set("Content-Type", record.MIMEType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", record.SizeBytes))
		w.Header().Set("Content-Disposition", contentDisposition(record.Name))
		_, _ = io.Copy(w, opened)
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
		before, after, limit, pageErr := messagePageParams(r)
		if pageErr != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, pageErr.Error())
			return
		}
		messages, hasMore, err := deps.Widget.MessagesPage(r.Context(), workspaceID, r.PathValue("id"), visitor, before, after, limit)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		out := make([]map[string]any, len(messages))
		for i, m := range messages {
			out[i] = widgetMessageJSONWithAttachments(r, deps, workspaceID, m)
		}
		response := map[string]any{"data": out, "has_more": hasMore, "next_cursor": nil}
		if hasMore && len(messages) > 0 && after == 0 {
			response["next_cursor"] = Cursor{Value: strconv.FormatInt(messages[0].Sequence, 10)}.Encode()
		}
		if hasMore && len(messages) > 0 && after > 0 {
			response["next_after"] = messages[len(messages)-1].Sequence
		}
		httpserver.WriteJSON(w, http.StatusOK, response)
	}
}

func handleWidgetPendingCommands(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		req := widgetVisitorRequest{PublicKey: query.Get("key"), URL: query.Get("url"), Token: query.Get("token")}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		limit, cursor, pageErr := PageParams(r)
		if pageErr != nil || (!cursor.IsZero() && (cursor.At.IsZero() || cursor.ID == "" || cursor.Value != "")) {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed command cursor.")
			return
		}
		page, err := deps.Widget.PendingCommandsPage(r.Context(), workspaceID, r.PathValue("id"), visitor, cursor.At, cursor.ID, limit)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		var nextCursor *string
		if page.HasMore && len(page.Items) > 0 {
			encoded := (Cursor{At: page.Items[len(page.Items)-1].CreatedAt, ID: page.Items[len(page.Items)-1].ID}).Encode()
			nextCursor = &encoded
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[widget.PendingCommand]{Data: page.Items, NextCursor: nextCursor, HasMore: page.HasMore})
	}
}

func writeWidgetError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, widget.ErrNotFound), errors.Is(err, conversation.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Not found.")
	case errors.Is(err, widget.ErrOriginNotAllowed):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This origin is not allowed to use this widget.")
	case errors.Is(err, widget.ErrVisitorInvalid), errors.Is(err, widget.ErrIdentityTokenInvalid), errors.Is(err, widget.ErrIdentityTokenExpired), errors.Is(err, widget.ErrIdentityTokenReplayed):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Your session has expired.")
	case errors.Is(err, widget.ErrConversationOwner):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This conversation does not belong to you.")
	case errors.Is(err, widget.ErrDisabled):
		httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "This widget is currently disabled.")
	case errors.Is(err, widget.ErrDuplicateDomain):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, formmodule.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Form not found.")
	case errors.Is(err, feedback.ErrNotFound):
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Feedback board or item not found.")
	case errors.Is(err, feedback.ErrAlreadyVoted), errors.Is(err, feedback.ErrVoteLimit), errors.Is(err, feedback.ErrVotingDisabled):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	case errors.Is(err, feedback.ErrCustomerRequired):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Identify yourself before following feedback.")
	case errors.Is(err, widget.ErrInvalidInbox), errors.Is(err, widget.ErrInvalidName),
		errors.Is(err, widget.ErrWildcardDomain), errors.Is(err, widget.ErrNoInbox),
		errors.Is(err, conversation.ErrEmptyBody):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, widget.ErrCommandBindingInvalid), errors.Is(err, widget.ErrCommandPayloadTooLarge):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, widget.ErrCommandBindingDisabled):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}

type commandBindingRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type commandBindingUpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Enabled     *bool   `json:"enabled"`
}

func handleUpdateCommandBinding(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req commandBindingUpdateRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed command binding.")
			return
		}
		actor := actorFromRequest(r)
		current, err := deps.Widget.ListCommandBindings(r.Context(), actor.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load command binding.")
			return
		}
		var existing *widget.CommandBinding
		for i := range current {
			if current[i].ID == r.PathValue("id") {
				existing = &current[i]
				break
			}
		}
		if existing == nil {
			writeWidgetError(w, r, widget.ErrNotFound)
			return
		}
		enabled := existing.Enabled
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		name := existing.Name
		if req.Name != nil {
			name = *req.Name
		}
		description := existing.Description
		if req.Description != nil {
			description = *req.Description
		}
		item, err := deps.Widget.UpdateCommandBinding(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), name, description, enabled)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, item)
	}
}

func handleListCommandBindings(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed command binding cursor.")
			return
		}
		items, err := deps.Widget.ListCommandBindingsPage(r.Context(), actorFromRequest(r).WorkspaceID, cursor.At, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load command bindings.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, NewPage(items, limit, func(item widget.CommandBinding) Cursor {
			return Cursor{At: item.CreatedAt, ID: item.ID}
		}))
	}
}

func handleCreateCommandBinding(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req commandBindingRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed command binding.")
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Widget.CreateCommandBinding(r.Context(), actor.WorkspaceID, actor.MemberID, req.Name, req.Description)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
			return
		}
		httpserver.WriteJSON(w, http.StatusCreated, item)
	}
}

type invokeCommandRequest struct {
	BindingID string         `json:"binding_id"`
	Payload   map[string]any `json:"payload"`
}

func handleInvokeCustomerCommand(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req invokeCommandRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed command invocation.")
			return
		}
		actor := actorFromRequest(r)
		item, err := deps.Widget.InvokeCommand(r.Context(), actor.WorkspaceID, actor.MemberID, r.PathValue("id"), req.BindingID, req.Payload)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusAccepted, item)
	}
}

type widgetCommandAckRequest struct {
	widgetVisitorRequest
	Status string `json:"status"`
}

func handleWidgetCommandAck(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req widgetCommandAckRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed command acknowledgement.")
			return
		}
		workspaceID, visitor, err := resolveVisitorRequest(r, deps, req.widgetVisitorRequest)
		if err != nil {
			writeWidgetError(w, r, err)
			return
		}
		if err := deps.Widget.AcknowledgeCommand(r.Context(), workspaceID, r.PathValue("id"), visitor, r.PathValue("commandID"), req.Status); err != nil {
			writeWidgetError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
