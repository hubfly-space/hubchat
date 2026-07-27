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
	"time"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

// Deps is every service the API layer calls into. Constructed once in
// cmd/hubchat and passed down — nothing in this package opens its own
// database connection.
type Deps struct {
	Pool         *database.Pool
	Logger       *slog.Logger
	Auth         *auth.Service
	Workspace    *workspace.Service
	Conversation *conversation.Service

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

	registerAuthRoutes(mux, deps)
	registerWorkspaceRoutes(mux, deps)
	registerConversationRoutes(mux, deps)

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
