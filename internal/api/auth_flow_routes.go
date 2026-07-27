package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/mailer"
)

// registerAuthFlowRoutes mounts everything beyond password sign-in: address
// verification, password reset, magic links, two-factor, and session
// management.
func registerAuthFlowRoutes(mux *http.ServeMux, deps Deps) {
	// Unauthenticated. These are the endpoints an attacker can reach without a
	// session, and each is written to reveal nothing about which addresses
	// have accounts.
	mux.HandleFunc("POST /v1/auth/password/forgot", handleForgotPassword(deps))
	mux.HandleFunc("POST /v1/auth/password/reset", handleResetPassword(deps))
	mux.HandleFunc("POST /v1/auth/magic-link", handleRequestMagicLink(deps))
	mux.HandleFunc("POST /v1/auth/magic-link/redeem", handleRedeemMagicLink(deps))
	mux.HandleFunc("POST /v1/auth/verify-email", handleVerifyEmail(deps))
	mux.HandleFunc("POST /v1/auth/totp/challenge", handleVerifyTOTPChallenge(deps))

	// Authenticated.
	mux.HandleFunc("POST /v1/auth/verify-email/resend", requireUser(deps, handleResendVerification(deps)))
	mux.HandleFunc("POST /v1/auth/password/change", requireUser(deps, handleChangePassword(deps)))
	mux.HandleFunc("GET /v1/auth/sessions", requireUser(deps, handleListSessions(deps)))
	mux.HandleFunc("DELETE /v1/auth/sessions/{id}", requireUser(deps, handleRevokeSession(deps)))
	mux.HandleFunc("POST /v1/auth/sessions/revoke-others", requireUser(deps, handleRevokeOtherSessions(deps)))
	mux.HandleFunc("GET /v1/auth/totp", requireUser(deps, handleTOTPStatus(deps)))
	mux.HandleFunc("POST /v1/auth/totp/begin", requireUser(deps, handleBeginTOTP(deps)))
	mux.HandleFunc("POST /v1/auth/totp/complete", requireUser(deps, handleCompleteTOTP(deps)))
	mux.HandleFunc("POST /v1/auth/totp/disable", requireUser(deps, handleDisableTOTP(deps)))
	mux.HandleFunc("PATCH /v1/auth/profile", requireUser(deps, handleUpdateProfile(deps)))
}

// ---------------------------------------------------------- password reset

type emailRequest struct {
	Email string `json:"email"`
	// Next is where to land after redeeming, carried through the link.
	Next string `json:"next"`
}

// handleForgotPassword always answers the same way.
//
// A different response for "no such account" would turn this form into an
// address-enumeration oracle, which is worth more to an attacker than the
// reset itself (§11.4). The work happens or does not; the client cannot tell.
func handleForgotPassword(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req emailRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		token, user, err := deps.Auth.IssuePasswordReset(r.Context(), req.Email)
		if err == nil {
			deps.sendMail(r, user.Email, "Reset your Hubchat password", "reset_password", mailer.Data{
				Name:      user.Name,
				Link:      deps.link("/app/reset-password", "token", token),
				ExpiresIn: "1 hour",
			})
		} else if !errors.Is(err, auth.ErrUserNotFound) {
			deps.Logger.Error("issuing password reset failed", "error", err)
		}

		acknowledge(w, r)
	}
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func handleResetPassword(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req resetPasswordRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		user, err := deps.Auth.ResetPassword(r.Context(), req.Token, req.Password)
		if err != nil {
			writeAuthFlowError(w, r, err)
			return
		}

		// Signed in straight away: the reset already proved control of the
		// mailbox, and sending someone to a login form to type the password
		// they just chose is friction with no security value.
		issueSession(w, r, deps, user.ID)
		httpserver.WriteJSON(w, http.StatusOK, userJSON(user))
	}
}

// ------------------------------------------------------------ magic links

func handleRequestMagicLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req emailRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		token, user, err := deps.Auth.IssueMagicLink(r.Context(), req.Email, safeNext(req.Next), clientIP(r))
		if err == nil {
			deps.sendMail(r, user.Email, "Your Hubchat sign-in link", "magic_link", mailer.Data{
				Name:      user.Name,
				Link:      deps.link("/app/magic-link", "token", token),
				ExpiresIn: "15 minutes",
			})
		} else if !errors.Is(err, auth.ErrUserNotFound) {
			deps.Logger.Error("issuing magic link failed", "error", err)
		}

		acknowledge(w, r)
	}
}

type tokenRequest struct {
	Token string `json:"token"`
}

func handleRedeemMagicLink(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req tokenRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		result, err := deps.Auth.RedeemMagicLink(r.Context(), req.Token, r.UserAgent(), clientIP(r))
		if err != nil {
			writeAuthFlowError(w, r, err)
			return
		}

		writeSignInResult(w, r, deps, result)
	}
}

// ------------------------------------------------------ email verification

func handleVerifyEmail(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req tokenRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		user, err := deps.Auth.VerifyEmail(r.Context(), req.Token)
		if err != nil {
			writeAuthFlowError(w, r, err)
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, userJSON(user))
	}
}

func handleResendVerification(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		token, _, err := deps.Auth.IssueEmailVerification(r.Context(), user.ID, user.Email)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError,
				"Could not send the verification email.")
			return
		}

		deps.sendMail(r, user.Email, "Confirm your email address", "verify_email", mailer.Data{
			Name:      user.Name,
			Link:      deps.link("/app/verify", "token", token),
			ExpiresIn: "24 hours",
		})
		acknowledge(w, r)
	}
}

// ------------------------------------------------------------------- TOTP

func handleTOTPStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		enabled, err := deps.Auth.TOTPEnabled(r.Context(), user.ID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not read your security settings.")
			return
		}

		remaining := 0
		if enabled {
			remaining, _ = deps.Auth.RemainingRecoveryCodes(r.Context(), user.ID)
		}

		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"enabled":                  enabled,
			"remaining_recovery_codes": remaining,
		})
	}
}

func handleBeginTOTP(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		secret, uri, err := deps.Auth.BeginTOTPEnrolment(r.Context(), user.ID, deps.issuerName())
		if err != nil {
			writeAuthFlowError(w, r, err)
			return
		}

		// The secret is returned to the browser so it can be shown as a QR
		// code and echoed back on completion. It is not yet stored: nothing is
		// enabled until the user proves their app produced a working code.
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"secret":           secret,
			"provisioning_uri": uri,
		})
	}
}

type completeTOTPRequest struct {
	Secret string `json:"secret"`
	Code   string `json:"code"`
}

func handleCompleteTOTP(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		var req completeTOTPRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		codes, err := deps.Auth.CompleteTOTPEnrolment(r.Context(), user.ID, req.Secret, req.Code)
		if err != nil {
			writeAuthFlowError(w, r, err)
			return
		}

		deps.recordUserAudit(r, audit.UserTOTPEnabled, user.ID)

		// The only time these exist in plaintext. The interface must make the
		// user save them before moving on.
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
	}
}

type passwordRequest struct {
	Password string `json:"password"`
}

func handleDisableTOTP(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		var req passwordRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		if err := deps.Auth.DisableTOTP(r.Context(), user.ID, req.Password); err != nil {
			writeAuthFlowError(w, r, err)
			return
		}

		deps.recordUserAudit(r, audit.UserTOTPDisabled, user.ID)
		w.WriteHeader(http.StatusNoContent)
	}
}

type totpChallengeRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

func handleVerifyTOTPChallenge(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req totpChallengeRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		result, err := deps.Auth.VerifyTOTPChallenge(
			r.Context(), req.Challenge, req.Code, r.UserAgent(), clientIP(r))
		if err != nil {
			writeAuthFlowError(w, r, err)
			return
		}

		writeSignInResult(w, r, deps, result)
	}
}

// --------------------------------------------------------------- sessions

func handleListSessions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		sessions, err := deps.Auth.ListSessions(r.Context(), user.ID, httpserver.SessionToken(r))
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not list your sessions.")
			return
		}

		out := make([]map[string]any, 0, len(sessions))
		for _, session := range sessions {
			out = append(out, map[string]any{
				"id":           session.ID,
				"user_agent":   session.UserAgent,
				"ip":           session.IP,
				"last_seen_at": session.LastSeenAt.UTC().Format(time.RFC3339),
				"created_at":   session.CreatedAt.UTC().Format(time.RFC3339),
				"expires_at":   session.ExpiresAt.UTC().Format(time.RFC3339),
				"current":      session.Current,
			})
		}
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
	}
}

func handleRevokeSession(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		if err := deps.Auth.RevokeSession(r.Context(), user.ID, r.PathValue("id")); err != nil {
			if errors.Is(err, auth.ErrSessionNotFound) {
				// 404 rather than 403: confirming that a session id exists but
				// belongs to someone else is itself a disclosure (§11.3).
				httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "No such session.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not revoke that session.")
			return
		}

		deps.recordUserAudit(r, audit.SessionRevoked, user.ID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeOtherSessions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		if err := deps.Auth.RevokeOtherSessions(r.Context(), user.ID, httpserver.SessionToken(r)); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not revoke your other sessions.")
			return
		}

		deps.recordUserAudit(r, audit.SessionRevoked, user.ID)
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------------------------------------------------------- account

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func handleChangePassword(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		var req changePasswordRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		err := deps.Auth.ChangePassword(
			r.Context(), user.ID, req.CurrentPassword, req.NewPassword, httpserver.SessionToken(r))
		if err != nil {
			writeAuthFlowError(w, r, err)
			return
		}

		deps.recordUserAudit(r, audit.UserPasswordChanged, user.ID)
		w.WriteHeader(http.StatusNoContent)
	}
}

type updateProfileRequest struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func handleUpdateProfile(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)

		var req updateProfileRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		name := strings.TrimSpace(req.Name)
		if name == "" {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "A name is required.")
			return
		}

		updated, err := deps.Auth.UpdateProfile(r.Context(), user.ID, name, req.AvatarURL)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not save your profile.")
			return
		}
		httpserver.WriteJSON(w, http.StatusOK, userJSON(updated))
	}
}

// ---------------------------------------------------------------- helpers

// writeSignInResult sets a session cookie, or reports the pending challenge.
//
// A challenge is a 200 with a body, not an error: the credentials *were*
// correct, and the client's next step is to collect a code — not to show a
// failure.
func writeSignInResult(w http.ResponseWriter, r *http.Request, deps Deps, result *auth.SignInResult) {
	if result.Challenge != nil {
		httpserver.WriteJSON(w, http.StatusOK, map[string]any{
			"challenge":  result.Challenge.Token,
			"expires_at": result.Challenge.ExpiresAt.UTC().Format(time.RFC3339),
		})
		return
	}

	httpserver.SetSessionCookie(w, result.Session.Token,
		int(deps.Auth.SessionLifetime().Seconds()), deps.CookieDomain, deps.CookieSecure)
	httpserver.WriteJSON(w, http.StatusOK, userJSON(result.User))
}

func userJSON(user *auth.User) map[string]any {
	return map[string]any{"id": user.ID, "name": user.Name, "email": user.Email}
}

// acknowledge answers a request whose outcome must not be observable.
func acknowledge(w http.ResponseWriter, _ *http.Request) {
	httpserver.WriteJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

// safeNext rejects redirect targets that leave this origin.
//
// A `next` parameter that accepts an absolute URL is an open redirect, and an
// open redirect on a sign-in flow is a phishing primitive: the link genuinely
// comes from us, and lands wherever the attacker chose. Only same-origin paths
// are allowed through.
func safeNext(next string) string {
	if next == "" {
		return ""
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return ""
	}
	return next
}

func writeAuthFlowError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrTokenInvalid):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, "token_invalid",
			"This link is invalid or has already been used.")
	case errors.Is(err, auth.ErrTokenExpired):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, "token_expired",
			"This link has expired. Request a new one.")
	case errors.Is(err, auth.ErrAccountLocked):
		httpserver.WriteError(w, r, http.StatusTooManyRequests, "account_locked",
			"Too many attempts. Wait a few minutes and try again.")
	case errors.Is(err, auth.ErrTOTPInvalid):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, "totp_invalid",
			"That code is not valid. Check your authenticator and try again.")
	case errors.Is(err, auth.ErrTOTPNotEnabled):
		httpserver.WriteError(w, r, http.StatusConflict, "totp_not_enabled",
			"Two-factor authentication is not enabled on this account.")
	case errors.Is(err, auth.ErrTOTPAlreadyOn):
		httpserver.WriteError(w, r, http.StatusConflict, "totp_already_enabled",
			"Two-factor authentication is already enabled.")
	case errors.Is(err, auth.ErrPasswordMismatch):
		httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError,
			"That is not your current password.")
	default:
		writeAuthError(w, r, err)
	}
}

var _ = fmt.Sprintf
