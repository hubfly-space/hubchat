// Package api wires the module services to HTTP.
//
// This is the "handler" layer docs/architecture.md's request lifecycle shows
// between routing and the service methods: it decodes a request, resolves who
// is calling and for which workspace, calls exactly one service method, and
// encodes the result. It holds no business logic of its own — a rule that
// exists so every decision here is unit-testable inside its owning module
// without an HTTP request in sight.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hubchat/hubchat/embedded"
	"github.com/hubchat/hubchat/internal/analytics"
	"github.com/hubchat/hubchat/internal/apikey"
	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/automation"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/emailchannel"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/feedback"
	"github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/form"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/hubchat/hubchat/internal/knowledgebase"
	"github.com/hubchat/hubchat/internal/notification"
	"github.com/hubchat/hubchat/internal/portability"
	"github.com/hubchat/hubchat/internal/portal"
	"github.com/hubchat/hubchat/internal/realtime"
	"github.com/hubchat/hubchat/internal/savedview"
	"github.com/hubchat/hubchat/internal/search"
	"github.com/hubchat/hubchat/internal/sla"
	"github.com/hubchat/hubchat/internal/survey"
	"github.com/hubchat/hubchat/internal/task"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/hubchat/hubchat/internal/webhook"
	"github.com/hubchat/hubchat/internal/widget"
	"github.com/hubchat/hubchat/internal/workspace"
)

// Deps is every service the API layer calls into. Constructed once in
// cmd/hubchat and passed down — nothing in this package opens its own
// database connection.
type Deps struct {
	Pool          *database.Pool
	Logger        *slog.Logger
	Auth          *auth.Service
	Workspace     *workspace.Service
	Conversation  *conversation.Service
	Inbox         *inbox.Service
	Customer      *customer.Service
	Search        *search.Service
	Ticket        *ticket.Service
	Widget        *widget.Service
	File          *file.Service
	Portal        *portal.Service
	Notification  *notification.Service
	Form          *form.Service
	APIKeys       *apikey.Service
	Webhook       *webhook.Service
	Knowledgebase *knowledgebase.Service
	Feedback      *feedback.Service
	Survey        *survey.Service
	SLA           *sla.Service
	Task          *task.Service
	Automation    *automation.Service
	SavedView     *savedview.Service
	Analytics     *analytics.Service
	EmailChannel  *emailchannel.Service
	Portability   *portability.Service

	// Hub answers "who is viewing this conversation right now" for the
	// Conversation DTO's presence field. Read-only from here — writes to
	// realtime state happen only through the WebSocket protocol itself
	// (internal/realtime), never from an HTTP handler.
	Hub *realtime.Hub

	// Shared infrastructure. Handlers reach these only for reads that have no
	// business logic behind them (the audit list, an entity's event timeline);
	// writes go through the owning module's service, which appends and audits
	// inside its own transaction.
	Events *events.Log
	Audit  *audit.Log
	Jobs   *jobs.Client

	// PublicURL is how browsers reach this deployment. Links in outbound email
	// are built from it rather than from the request Host, which an attacker
	// controls (§11.4).
	PublicURL *url.URL

	// Config backs the /v1/setup/state diagnostics the first-run wizard reads.
	// Handlers otherwise reach for the narrower fields above rather than this
	// — it exists for the one screen that legitimately needs a deployment-wide
	// view, not as a general escape hatch around Deps' explicit surface.
	Config config.Config

	CookieDomain string
	CookieSecure bool
}

// New builds the /api/v1 router. Routes are written relative to this
// handler's root — the caller (httpserver.New) mounts it with the "/api"
// prefix already stripped.
func New(deps Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/meta", func(w http.ResponseWriter, r *http.Request) {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"version": "v1", "surface": "api"})
	})
	mux.HandleFunc("GET /v1/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(embedded.OpenAPI())
	})

	registerAuthRoutes(mux, deps)
	registerAuthFlowRoutes(mux, deps)
	registerSetupRoutes(mux, deps)
	registerBootstrapRoutes(mux, deps)
	registerWorkspaceRoutes(mux, deps)
	registerMemberRoutes(mux, deps)
	registerSCIMRoutes(mux, deps)
	registerInviteRoutes(mux, deps)
	registerTeamRoutes(mux, deps)
	registerSettingsRoutes(mux, deps)
	registerTagRoutes(mux, deps)
	registerAuditRoutes(mux, deps)
	registerConversationRoutes(mux, deps)
	registerSavedViewRoutes(mux, deps)
	registerInboxRoutes(mux, deps)
	registerCustomerRoutes(mux, deps)
	registerCompanyRoutes(mux, deps)
	registerSearchRoutes(mux, deps)
	registerTicketRoutes(mux, deps)
	registerWidgetRoutes(mux, deps)
	registerFileRoutes(mux, deps)
	registerPortalRoutes(mux, deps)
	registerPortalAdminRoutes(mux, deps)
	registerNotificationRoutes(mux, deps)
	registerFormRoutes(mux, deps)
	registerAPIKeyRoutes(mux, deps)
	registerWebhookRoutes(mux, deps)
	registerKnowledgeBaseRoutes(mux, deps)
	registerFeedbackRoutes(mux, deps)
	registerSurveyRoutes(mux, deps)
	registerSLARoutes(mux, deps)
	registerTaskRoutes(mux, deps)
	registerAutomationRoutes(mux, deps)
	registerAnalyticsRoutes(mux, deps)
	registerEmailChannelRoutes(mux, deps)
	registerJobRoutes(mux, deps)
	registerOpsRoutes(mux, deps)
	registerPortabilityRoutes(mux, deps)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound,
			"No API route matches this path.")
	})

	return mux
}

// Ready is used by the /readyz endpoint (see httpserver.Routes.Ready).
func (d Deps) Ready(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return d.Pool.Ping(checkCtx)
}

// requireActor resolves the session cookie to a user, then resolves a
// workspace and builds the authorization.Actor for it, attaching both to the
// request context. Handlers behind this middleware can assume
// authorization.FromContext(ctx) is non-nil.
//
// Workspace selection: the "Hubchat-Workspace-Id" header names one
// explicitly (what the dashboard sends once a user has picked a workspace in
// the switcher); absent that, the user's first membership is used, which
// covers the common case of a single-workspace account and the setup flow
// immediately after it.
func requireActor(deps Deps, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httpserver.SessionToken(r)
		if token == "" {
			authorizationHeader := strings.TrimSpace(r.Header.Get("Authorization"))
			bearer := ""
			if strings.HasPrefix(authorizationHeader, "Bearer ") {
				bearer = strings.TrimSpace(strings.TrimPrefix(authorizationHeader, "Bearer "))
			}
			if deps.APIKeys != nil && bearer != "" {
				principal, keyErr := deps.APIKeys.Authenticate(r.Context(), bearer)
				if keyErr != nil {
					httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "The API key is invalid or expired.")
					return
				}
				if requested := r.Header.Get("Hubchat-Workspace-Id"); requested != "" && requested != principal.WorkspaceID {
					httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "The API key does not belong to this workspace.")
					return
				}
				capabilities := make(map[authorization.Capability]bool, len(principal.Scopes))
				for _, scope := range principal.Scopes {
					capabilities[authorization.Capability(scope)] = true
				}
				memberID := ""
				if principal.CreatedBy != nil {
					memberID = *principal.CreatedBy
				}
				actor := &authorization.Actor{MemberID: memberID, WorkspaceID: principal.WorkspaceID, Role: "api_key", Capabilities: capabilities}
				ctx := authorization.WithActor(r.Context(), actor)
				next(w, r.WithContext(ctx))
				return
			}
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Sign in to continue.")
			return
		}

		user, err := deps.Auth.UserForSession(r.Context(), token)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized,
				"Your session has expired. Sign in again.")
			return
		}

		workspaceID := r.Header.Get("Hubchat-Workspace-Id")
		if workspaceID == "" {
			workspaceID, err = deps.Workspace.DefaultWorkspaceID(r.Context(), user.ID)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden,
					"You do not belong to a workspace yet.")
				return
			}
		}

		actor, err := deps.Workspace.ActorForUser(r.Context(), workspaceID, user.ID)
		if err != nil {
			// Either the workspace does not exist or this user is not a member
			// of it — both are the same "you may not be here" answer to the
			// client (§11.3, §11.6: a missing tenant predicate is a critical
			// defect, and so is trusting a client-supplied workspace id without
			// checking membership, which is exactly what ActorForUser checks).
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden,
				"You do not have access to this workspace.")
			return
		}

		settings, err := deps.Workspace.GetSettings(r.Context(), workspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load workspace security policy.")
			return
		}
		if settings.Security.RequireSSO && user.AuthMethod != auth.AuthMethodOAuth {
			// Do not leave a valid password session attached to a workspace that
			// requires organization SSO. Clearing the browser cookie makes the
			// dashboard's existing 401 flow return to the provider button while
			// preserving the server-side session for other workspaces.
			httpserver.ClearSessionCookie(w, deps.CookieDomain, deps.CookieSecure)
			httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized,
				"This workspace requires organization SSO. Sign in with your provider.")
			return
		}

		ctx := authorization.WithActor(r.Context(), actor)
		next(w, r.WithContext(ctx))
	}
}

// requireCapability wraps requireActor and additionally checks a capability
// (§11.3). Handlers for anything beyond read-your-own-inbox should use this
// rather than requireActor alone.
func requireCapability(deps Deps, capability authorization.Capability, next http.HandlerFunc) http.HandlerFunc {
	return requireActor(deps, func(w http.ResponseWriter, r *http.Request) {
		actor := authorization.FromContext(r.Context())
		if !actor.Can(capability) {
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden,
				"You do not have permission to do that.")
			return
		}
		next(w, r)
	})
}
