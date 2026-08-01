package httpserver

import "net/http"

// SessionCookieName is the cookie the dashboard's session lives in. Named
// here, rather than inside internal/auth, because the HTTP-only detail (a
// cookie) belongs to the transport layer — auth only knows about tokens.
const SessionCookieName = "hubchat_session"

// TOTPChallengeCookieName carries only the short-lived half-authenticated
// OAuth state. It is HttpOnly so the challenge bearer never enters the URL or
// browser storage; the TOTP endpoint consumes it server-side.
const TOTPChallengeCookieName = "hubchat_totp_challenge"

const TrustedDeviceCookieName = "hubchat_trusted_device"

// SetSessionCookie writes the session cookie with the flags §11.4 requires:
// HttpOnly (no client-script access), Secure in production, SameSite=Lax
// (survives a top-level navigation from an email link, blocks cross-site
// POST forgery).
func SetSessionCookie(w http.ResponseWriter, token string, maxAge int, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Domain:   domain,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the cookie immediately, used on logout.
func ClearSessionCookie(w http.ResponseWriter, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// SessionToken reads the raw token from the request, or "" if absent.
func SessionToken(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func SetTOTPChallengeCookie(w http.ResponseWriter, token string, maxAge int, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: TOTPChallengeCookieName, Value: token, Path: "/", Domain: domain,
		MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func ClearTOTPChallengeCookie(w http.ResponseWriter, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: TOTPChallengeCookieName, Value: "", Path: "/", Domain: domain,
		MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func TOTPChallengeToken(r *http.Request) string {
	cookie, err := r.Cookie(TOTPChallengeCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func SetTrustedDeviceCookie(w http.ResponseWriter, token string, maxAge int, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: TrustedDeviceCookieName, Value: token, Path: "/", Domain: domain,
		MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func ClearTrustedDeviceCookie(w http.ResponseWriter, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: TrustedDeviceCookieName, Value: "", Path: "/", Domain: domain,
		MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func TrustedDeviceToken(r *http.Request) string {
	cookie, err := r.Cookie(TrustedDeviceCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
