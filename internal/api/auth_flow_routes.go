package api

import (
	"errors"
	"net/http"
	"net/url"
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
	mux.HandleFunc("GET /v1/auth/oauth/{provider}/start", handleOAuthStart(deps))
	mux.HandleFunc("GET /v1/auth/oauth/{provider}/callback", handleOAuthCallback(deps))

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
	mux.HandleFunc("GET /v1/auth/trusted-devices", requireUser(deps, handleListTrustedDevices(deps)))
	mux.HandleFunc("DELETE /v1/auth/trusted-devices/{id}", requireUser(deps, handleRevokeTrustedDevice(deps)))
	mux.HandleFunc("POST /v1/auth/trusted-devices/revoke-all", requireUser(deps, handleRevokeAllTrustedDevices(deps)))
	mux.HandleFunc("PATCH /v1/auth/profile", requireUser(deps, handleUpdateProfile(deps)))
}

func handleOAuthStart(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configured := deps.Auth.OAuthProvider()
		if configured == nil || configured.Provider != r.PathValue("provider") || deps.PublicURL == nil {
			http.Redirect(w, r, oauthFailureURL(deps, "unavailable"), http.StatusFound)
			return
		}
		stateURL, _, err := deps.Auth.BeginOAuth(r.Context(), safeNext(r.URL.Query().Get("next")))
		if err != nil {
			deps.Logger.Error("starting oauth sign-in failed", "error", err)
			http.Redirect(w, r, oauthFailureURL(deps, "unavailable"), http.StatusFound)
			return
		}
		http.Redirect(w, r, stateURL, http.StatusFound)
	}
}

func handleOAuthCallback(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configured := deps.Auth.OAuthProvider()
		if configured == nil || configured.Provider != r.PathValue("provider") {
			http.Redirect(w, r, oauthFailureURL(deps, "unavailable"), http.StatusFound)
			return
		}
		if r.URL.Query().Get("error") != "" {
			http.Redirect(w, r, oauthFailureURL(deps, "cancelled"), http.StatusFound)
			return
		}
		result, err := deps.Auth.CompleteOAuthWithTrustedDevice(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"), r.UserAgent(), clientIP(r), httpserver.TrustedDeviceToken(r))
		if err != nil {
			deps.Logger.Warn("oauth sign-in failed", "provider", configured.Provider, "error", err)
			http.Redirect(w, r, oauthFailureURL(deps, oauthErrorCode(err)), http.StatusFound)
			return
		}
		if result.Challenge != nil {
			target := safeNext(result.Redirect)
			if target == "" {
				target = "/overview"
			}
			httpserver.SetTOTPChallengeCookie(w, result.Challenge.Token, 5*60, deps.CookieDomain, deps.CookieSecure)
			challenge := url.Values{}
			challenge.Set("pending", "1")
			challenge.Set("next", target)
			http.Redirect(w, r, dashboardURL(deps, "/two-factor", challenge), http.StatusFound)
			return
		}
		if result.TrustedDevice != nil {
			httpserver.SetTrustedDeviceCookie(w, result.TrustedDevice.Token, int(time.Until(result.TrustedDevice.ExpiresAt).Seconds()), deps.CookieDomain, deps.CookieSecure)
		}
		httpserver.SetSessionCookie(w, result.Session.Token, int(deps.Auth.SessionLifetime().Seconds()), deps.CookieDomain, deps.CookieSecure)
		target := safeNext(result.Redirect)
		if target == "" {
			target = "/overview"
		}
		http.Redirect(w, r, dashboardURL(deps, target, nil), http.StatusFound)
	}
}

func oauthErrorCode(err error) string {
	switch {
	case errors.Is(err, auth.ErrOAuthStateInvalid):
		return "expired"
	case errors.Is(err, auth.ErrOAuthEmailUnverified), errors.Is(err, auth.ErrOAuthDomainDenied):
		return "not_allowed"
	default:
		return "failed"
	}
}

func oauthFailureURL(deps Deps, code string) string {
	values := url.Values{}
	values.Set("oauth_error", code)
	return dashboardURL(deps, "/login", values)
}

func dashboardURL(deps Deps, path string, values url.Values) string {
	if deps.PublicURL == nil {
		if len(values) == 0 {
			return path
		}
		return path + "?" + values.Encode()
	}
	target := *deps.PublicURL
	target.Path = strings.TrimSuffix(target.Path, "/") + "/app" + path
	target.RawQuery = ""
	if len(values) > 0 {
		target.RawQuery = values.Encode()
	}
	return target.String()
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

		next := safeNext(req.Next)
		token, user, err := deps.Auth.IssueMagicLink(r.Context(), req.Email, next, clientIP(r))
		if err == nil {
			deps.sendMail(r, user.Email, "Your Hubchat sign-in link", "magic_link", mailer.Data{
				Name:      user.Name,
				Link:      authMagicLink(deps, token, next),
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

		result, err := deps.Auth.RedeemMagicLinkWithTrustedDevice(r.Context(), req.Token, r.UserAgent(), clientIP(r), httpserver.TrustedDeviceToken(r))
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
	Challenge   string `json:"challenge"`
	Code        string `json:"code"`
	TrustDevice bool   `json:"trust_device"`
}

func handleVerifyTOTPChallenge(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req totpChallengeRequest
		if err := httpserver.DecodeJSON(r, &req); err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed request body.")
			return
		}

		result, err := deps.Auth.VerifyTOTPChallengeWithTrust(
			r.Context(), firstNonEmpty(req.Challenge, httpserver.TOTPChallengeToken(r)), req.Code, r.UserAgent(), clientIP(r), req.TrustDevice)
		if err != nil {
			writeAuthFlowError(w, r, err)
			return
		}

		writeSignInResult(w, r, deps, result)
		if result.Session != nil {
			httpserver.ClearTOTPChallengeCookie(w, deps.CookieDomain, deps.CookieSecure)
		}
	}
}

func handleListTrustedDevices(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}
		devices, err := deps.Auth.ListTrustedDevicesPage(r.Context(), user.ID, httpserver.TrustedDeviceToken(r), cursor.At, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load trusted devices.")
			return
		}
		page := NewPage(devices, limit, func(device auth.TrustedDeviceInfo) Cursor {
			return Cursor{At: device.CreatedAt, ID: device.ID}
		})
		out := make([]map[string]any, 0, len(page.Data))
		for _, device := range page.Data {
			out = append(out, map[string]any{
				"id": device.ID, "name": device.Name, "user_agent": device.UserAgent,
				"ip": device.IP, "created_at": device.CreatedAt.UTC().Format(time.RFC3339),
				"last_used_at": nullableTime(device.LastUsedAt), "expires_at": device.ExpiresAt.UTC().Format(time.RFC3339),
				"current": device.Current,
			})
		}
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
	}
}

func handleRevokeTrustedDevice(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)
		current := false
		if token := httpserver.TrustedDeviceToken(r); token != "" {
			devices, err := deps.Auth.ListTrustedDevices(r.Context(), user.ID, token)
			if err != nil {
				httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load trusted devices.")
				return
			}
			for _, device := range devices {
				if device.ID == r.PathValue("id") {
					current = device.Current
					break
				}
			}
		}
		if err := deps.Auth.RevokeTrustedDevice(r.Context(), user.ID, r.PathValue("id")); err != nil {
			if errors.Is(err, auth.ErrTrustedDeviceNotFound) {
				httpserver.WriteError(w, r, http.StatusNotFound, httpserver.CodeNotFound, "Trusted device not found.")
				return
			}
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not revoke that trusted device.")
			return
		}
		if current {
			// Revoking the current device must also remove its browser credential.
			httpserver.ClearTrustedDeviceCookie(w, deps.CookieDomain, deps.CookieSecure)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeAllTrustedDevices(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)
		if err := deps.Auth.RevokeAllTrustedDevices(r.Context(), user.ID); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not revoke trusted devices.")
			return
		}
		httpserver.ClearTrustedDeviceCookie(w, deps.CookieDomain, deps.CookieSecure)
		w.WriteHeader(http.StatusNoContent)
	}
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// --------------------------------------------------------------- sessions

func handleListSessions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)
		limit, cursor, err := PageParams(r)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusBadRequest, httpserver.CodeBadRequest, "Malformed cursor.")
			return
		}

		sessions, err := deps.Auth.ListSessionsPage(r.Context(), user.ID, httpserver.SessionToken(r), cursor.At, cursor.ID, limit+1)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not list your sessions.")
			return
		}

		page := NewPage(sessions, limit, func(session auth.SessionInfo) Cursor {
			return Cursor{At: session.LastSeenAt, ID: session.ID}
		})
		out := make([]map[string]any, 0, len(page.Data))
		for _, session := range page.Data {
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
		httpserver.WriteJSON(w, http.StatusOK, Page[map[string]any]{Data: out, NextCursor: page.NextCursor, HasMore: page.HasMore})
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

	if result.TrustedDevice != nil {
		httpserver.SetTrustedDeviceCookie(w, result.TrustedDevice.Token, int(time.Until(result.TrustedDevice.ExpiresAt).Seconds()), deps.CookieDomain, deps.CookieSecure)
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
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return ""
	}
	parsed, err := url.Parse(next)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return ""
	}
	return parsed.String()
}

func authMagicLink(deps Deps, token, next string) string {
	values := url.Values{}
	values.Set("token", token)
	if next != "" {
		values.Set("next", next)
	}

	if deps.PublicURL == nil {
		return "/app/magic-link?" + values.Encode()
	}

	target := *deps.PublicURL
	target.Path = strings.TrimSuffix(target.Path, "/") + "/app/magic-link"
	target.RawQuery = values.Encode()
	return target.String()
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
