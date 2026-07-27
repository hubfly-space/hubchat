package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// RequestID assigns every request an identifier, echoes it on the response,
// and puts it in the context.
//
// It is the thread that ties an error the user saw, a line in the logs, an
// audit entry, and a webhook delivery together. §16 requires it on every API
// response; this middleware makes that automatic rather than per-handler.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		// A client-supplied value is only trusted for correlation, never for
		// anything security-relevant, and is length-capped so it cannot be used
		// to bloat log lines.
		if id == "" || len(id) > 64 {
			id = newRequestID()
		}

		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// RequestIDFrom returns the identifier assigned to this request, if any.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// Recover turns a panic into a 500 rather than a dropped connection, and logs
// the stack once. Without it a single nil dereference in one handler takes the
// whole server down, which for a single-binary deployment means taking support
// offline entirely.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					if recovered == http.ErrAbortHandler {
						panic(recovered) // deliberate abort; let it through
					}

					logger.ErrorContext(r.Context(), "panic recovered",
						slog.Any("panic", recovered),
						slog.String("path", r.URL.Path),
						slog.String("request_id", RequestIDFrom(r.Context())),
						slog.String("stack", string(debug.Stack())),
					)

					WriteError(w, r, http.StatusInternalServerError, "internal_error",
						"Something went wrong on our side.")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// Logger emits one structured line per request.
//
// It deliberately records no message bodies, query strings, or headers beyond
// the method and path (§19): a support platform's request bodies are, by
// definition, other people's customer conversations.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			level := slog.LevelInfo
			switch {
			case recorder.status >= 500:
				level = slog.LevelError
			case recorder.status >= 400:
				level = slog.LevelWarn
			}

			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.status),
				slog.Int64("bytes", recorder.written),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", RequestIDFrom(r.Context())),
			)
		})
	}
}

// SecurityHeaders applies the defaults from §11.4.
//
// The content security policy is set per surface rather than globally, because
// the three browser bundles have genuinely different needs: the dashboard talks
// only to its own origin, the portal may embed customer-supplied images, and
// the widget is loaded cross-origin by design.
func SecurityHeaders(surface Surface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			header.Set("Cross-Origin-Opener-Policy", "same-origin")

			switch surface {
			case SurfaceDashboard:
				// The dashboard must never be framed — clickjacking it means
				// clickjacking every conversation in the workspace.
				header.Set("X-Frame-Options", "DENY")
				header.Set("Content-Security-Policy", strings.Join([]string{
					"default-src 'self'",
					"script-src 'self' 'unsafe-inline'", // inline theme bootstrap; nonce in production
					"style-src 'self' 'unsafe-inline'",
					"img-src 'self' data: blob: https:",
					"connect-src 'self' ws: wss:",
					"font-src 'self'",
					"object-src 'none'",
					"base-uri 'self'",
					"form-action 'self'",
					"frame-ancestors 'none'",
				}, "; "))

			case SurfacePortal:
				header.Set("X-Frame-Options", "SAMEORIGIN")
				header.Set("Content-Security-Policy", strings.Join([]string{
					"default-src 'self'",
					"script-src 'self' 'unsafe-inline'",
					"style-src 'self' 'unsafe-inline'",
					"img-src 'self' data: https:",
					"connect-src 'self' ws: wss:",
					"object-src 'none'",
					"base-uri 'self'",
					"form-action 'self'",
				}, "; "))

			case SurfaceWidget:
				// Framing is the point. Origin control for the widget comes
				// from the per-widget domain allowlist checked when the
				// configuration is requested, not from a frame-ancestors rule
				// that cannot know the tenant at this layer.
				header.Set("Cross-Origin-Resource-Policy", "cross-origin")

			case SurfaceAPI:
				header.Set("X-Frame-Options", "DENY")
				// API responses are JSON. A restrictive policy costs nothing
				// and stops a stray HTML error page from executing anything.
				header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Surface identifies which browser-facing area a route belongs to.
type Surface int

const (
	SurfaceAPI Surface = iota
	SurfaceDashboard
	SurfacePortal
	SurfaceWidget
)

// MaxBytes caps request bodies before a handler reads them, so an oversized
// upload is rejected rather than buffered (§17: no unbounded memory growth).
func MaxBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// Chain applies middleware so that the first argument is the outermost layer,
// which reads in the same order it executes.
func Chain(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written int64
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

// Unwrap lets http.NewResponseController see through this wrapper to the
// underlying ResponseWriter's Hijacker, Flusher, etc. Without it, wrapping the
// response writer here silently breaks the WebSocket upgrade: coder/websocket
// hijacks the connection via exactly this mechanism (the standard library's
// response-writer-wrapping convention since Go 1.20), and a wrapper with no
// Unwrap looks to it like a ResponseWriter that cannot be hijacked at all.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func newRequestID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		// A duplicate correlation id is a nuisance; failing the request over it
		// would be worse.
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(buf)
}
