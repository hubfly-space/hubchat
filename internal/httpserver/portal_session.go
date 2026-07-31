package httpserver

import "net/http"

// PortalSessionCookieName is intentionally different from SessionCookieName:
// portal customers and workspace members are different security principals.
const PortalSessionCookieName = "hubchat_portal_session"

func SetPortalSessionCookie(w http.ResponseWriter, token string, maxAge int, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: PortalSessionCookieName, Value: token, Path: "/", Domain: domain,
		MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func ClearPortalSessionCookie(w http.ResponseWriter, domain string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: PortalSessionCookieName, Value: "", Path: "/", Domain: domain,
		MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func PortalSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(PortalSessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
