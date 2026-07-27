package api

import (
	"errors"
	"net/http"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func registerAuthRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("POST /v1/auth/signup", handleSignup(deps))
	mux.HandleFunc("POST /v1/auth/login", handleLogin(deps))
	mux.HandleFunc("POST /v1/auth/logout", handleLogout(deps))
	mux.HandleFunc("GET /v1/auth/me", requireActor(deps, handleMe(deps)))
}

type signupRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func handleSignup(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req signupRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		user, err := deps.Auth.SignUp(r.Context(), req.Name, req.Email, req.Password)
		if err != nil {
			writeAuthError(w, r, err)
			return
		}

		issueSession(w, r, deps, user.ID)
		httpserver.WriteJSON(w, http.StatusCreated, map[string]any{
			"id": user.ID, "name": user.Name, "email": user.Email,
		})
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func handleLogin(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		user, err := deps.Auth.Authenticate(r.Context(), req.Email, req.Password)
		if err != nil {
			writeAuthError(w, r, err)
			return
		}

		issueSession(w, r, deps, user.ID)
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"id": user.ID, "name": user.Name, "email": user.Email,
		})
	}
}

func handleLogout(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token := httpserver.SessionToken(r); token != "" {
			_ = deps.Auth.Logout(r.Context(), token)
		}
		httpserver.ClearSessionCookie(w, deps.CookieDomain, deps.CookieSecure)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMe(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// requireActor already resolved the user and workspace; re-fetching the
		// user row here would be redundant so this endpoint's response is built
		// straight from the actor plus a workspace lookup.
		actor := actorFromRequest(r)

		ws, err := deps.Workspace.Get(r.Context(), actor.WorkspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load workspace.")
			return
		}

		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"member_id":    actor.MemberID,
			"role":         actor.Role,
			"workspace": map[string]any{
				"id":   ws.ID,
				"name": ws.Name,
				"slug": ws.Slug,
			},
		})
	}
}

func issueSession(w http.ResponseWriter, r *http.Request, deps Deps, userID string) {
	session, err := deps.Auth.CreateSession(r.Context(), userID, r.UserAgent(), clientIP(r))
	if err != nil {
		// Session creation failing after a successful signup/login is an
		// internal problem, not a credentials problem — the account exists
		// either way, so the client should retry rather than being told their
		// password is wrong.
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError,
			"Signed in, but starting your session failed. Try again.")
		return
	}
	httpserver.SetSessionCookie(w, session.Token, int(deps.Auth.SessionLifetime().Seconds()), deps.CookieDomain, deps.CookieSecure)
}

func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		httpserver.WriteError(w, r, http.StatusUnauthorized, httpserver.CodeUnauthorized, "Incorrect email or password.")
	case errors.Is(err, auth.ErrEmailTaken):
		httpserver.WriteError(w, r, http.StatusConflict, httpserver.CodeConflict, "An account with this email already exists.")
	case errors.Is(err, auth.ErrWeakPassword):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	case errors.Is(err, auth.ErrInvalidEmail):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, err.Error())
	default:
		httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Something went wrong.")
	}
}

func clientIP(r *http.Request) string {
	// XFF is only trusted because the deployment's reverse proxy is expected to
	// set it and strip any client-supplied value first; see docs/security.md
	// §6 — a hardened deployment restricts this via Security.TrustedProxies.
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	host, _, err := splitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}
