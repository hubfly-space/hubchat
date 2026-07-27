package httpserver

import (
	"net/http"
	"net/url"
	"strings"
)

// CSRF rejects cookie-authenticated state changes that did not come from our
// own origin.
//
// The check is origin-based rather than token-based, and that choice is worth
// stating plainly because the token pattern is more familiar.
//
// A synchroniser token proves the request came from a page we rendered. Origin
// checking proves the request came from our origin. For a JSON API consumed by
// a single-page app, the second is both sufficient and less to get wrong:
// there is no hidden field to thread through every form, no token to rotate on
// login, and no way for a handler to forget it. Browsers set Origin on every
// cross-site state-changing request and refuse to let script forge it.
//
// What makes this sound is the combination with SameSite=Lax on the session
// cookie: Lax already blocks the cookie from riding along on cross-site POSTs,
// and this is the second layer for the cases Lax does not cover (older
// browsers, and same-site-but-different-origin subdomains).
//
// Requests authenticated by an API key rather than a cookie are exempt — they
// are not subject to CSRF at all, because the credential is not something a
// browser attaches automatically.
func CSRF(publicURL *url.URL, enabled bool) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	if publicURL != nil {
		allowed[strings.ToLower(publicURL.Scheme+"://"+publicURL.Host)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled || isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			// Bearer-authenticated calls carry no ambient credential, so there
			// is nothing for a hostile page to ride on.
			if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}

			// No session cookie means no ambient credential either.
			if SessionToken(r) == "" {
				next.ServeHTTP(w, r)
				return
			}

			origin := requestOrigin(r)
			if origin == "" {
				// A same-origin fetch from a browser always sets one of Origin
				// or Referer on a state-changing request. Absence means either
				// a non-browser client (which should be using an API key) or a
				// browser deliberately stripping it.
				WriteError(w, r, http.StatusForbidden, CodeForbidden,
					"This request is missing its origin and cannot be verified.")
				return
			}

			if !allowed[origin] {
				WriteError(w, r, http.StatusForbidden, CodeForbidden,
					"This request came from an unrecognised origin.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// requestOrigin reads Origin, falling back to the scheme and host of Referer.
//
// The fallback matters for same-origin form posts, where some browsers send
// Referer but omit Origin.
func requestOrigin(r *http.Request) string {
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
		return strings.ToLower(origin)
	}

	referer := r.Header.Get("Referer")
	if referer == "" {
		return ""
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
