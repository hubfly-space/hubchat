// Package httpserver owns the HTTP surface: routing, middleware, asset
// serving, and the error contract.
//
// It contains no business logic. Handlers unmarshal a request, call a service
// method on the owning module, and marshal the result — §14's boundary rule.
// If a handler is making decisions, that decision belongs in a module where it
// can be unit-tested without a request.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/config"
)

// Assets bundles the three compiled browser applications.
type Assets struct {
	Dashboard fs.FS
	Portal    fs.FS
	Widget    fs.FS
}

// Routes carries the handlers assembled by internal/api, which owns the
// business-logic wiring this package deliberately does not (see the package
// doc). Either field may be nil — a build without a database configured still
// serves the compiled frontends and health checks.
type Routes struct {
	// API is mounted at /api/, with /api stripped, so its own routes are
	// written as if API were the root ("/v1/conversations", not
	// "/api/v1/conversations").
	API http.Handler
	// WS is mounted at /ws/.
	WS http.Handler
	// Ready is called by /readyz. Nil means "no dependency to check" — used
	// when the server was started without a database (§18 health endpoints).
	Ready func(context.Context) error
}

// Server wraps the standard library's HTTP server with this project's routing
// and lifecycle.
type Server struct {
	cfg    config.Config
	logger *slog.Logger
	http   *http.Server
}

// New builds the router and returns a server ready to Start.
func New(cfg config.Config, logger *slog.Logger, assets Assets, routes Routes) (*Server, error) {
	mux := http.NewServeMux()

	// ------------------------------------------------------------- health
	//
	// Liveness and readiness are separate on purpose. Liveness answers "is this
	// process wedged?" and must never touch a dependency, or a brief database
	// blip gets the container killed. Readiness answers "should traffic come
	// here?" and does check dependencies.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if routes.Ready == nil {
			WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "database": "not_configured"})
			return
		}

		checkCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if err := routes.Ready(checkCtx); err != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status":   "unavailable",
				"database": "error",
				"error":    err.Error(),
			})
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "database": "ok"})
	})

	// ---------------------------------------------------------------- API
	apiHandler := routes.API
	if apiHandler == nil {
		// No database configured (e.g. `hubchat doctor` builds a server just to
		// check routing) — serve a clear message instead of a bare 404 pointing
		// nowhere.
		apiHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			WriteError(w, r, http.StatusServiceUnavailable, CodeUnavailable,
				"The API is not available: this server was started without a database connection.")
		})
	}

	mux.Handle("/api/", Chain(
		http.StripPrefix("/api", apiHandler),
		SecurityHeaders(SurfaceAPI),
		MaxBytes(cfg.Server.MaxRequestBytes),
	))

	// ----------------------------------------------------------- realtime
	if routes.WS != nil {
		mux.Handle("/ws/", Chain(
			http.StripPrefix("/ws", routes.WS),
			SecurityHeaders(SurfaceAPI),
		))
	}

	// ------------------------------------------------------------- widget
	//
	// Mounted before the dashboard so its cross-origin headers apply, and kept
	// deliberately free of session cookies — the widget authenticates with a
	// scoped visitor token, never with an agent's session.
	if assets.Widget != nil {
		widget := NewStaticHandler(assets.Widget, "/widget", cfg.Dev)
		mux.Handle("/widget/", Chain(widget, SecurityHeaders(SurfaceWidget)))
	}

	// ------------------------------------------------------------- portal
	if assets.Portal != nil {
		portal, err := NewSPAHandler(assets.Portal, "/portal", cfg.Dev)
		if err != nil {
			return nil, err
		}
		mux.Handle("/portal/", Chain(portal, SecurityHeaders(SurfacePortal)))
	}

	// ---------------------------------------------------------- dashboard
	if assets.Dashboard != nil {
		dashboard, err := NewSPAHandler(assets.Dashboard, "/app", cfg.Dev)
		if err != nil {
			return nil, err
		}
		mux.Handle("/app/", Chain(dashboard, SecurityHeaders(SurfaceDashboard)))
	}

	// The root sends people where they belong. A self-hosted deployment's bare
	// domain is most often opened by an operator looking for the dashboard.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	handler := Chain(mux,
		RequestID,
		Recover(logger),
		Logger(logger),
	)

	return &Server{
		cfg:    cfg,
		logger: logger,
		http: &http.Server{
			Addr:              cfg.Server.Listen,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       cfg.Server.ReadTimeout,
			WriteTimeout:      cfg.Server.WriteTimeout,
			IdleTimeout:       cfg.Server.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		},
	}, nil
}

// Start listens until the context is cancelled, then shuts down gracefully.
//
// Graceful means in-flight requests finish and WebSocket clients are told to
// reconnect, rather than having connections severed mid-reply. §18 requires it;
// for a support tool it is the difference between "the page blipped" and "my
// message vanished".
func (s *Server) Start(ctx context.Context) error {
	errs := make(chan error, 1)

	go func() {
		s.logger.Info("http server listening",
			slog.String("addr", s.http.Addr),
			slog.Bool("dev", s.cfg.Dev),
		)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}

	s.logger.Info("http server shutting down",
		slog.Duration("grace", s.cfg.Server.ShutdownTimeout))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		// Requests still in flight past the grace period. Report it rather than
		// pretending the shutdown was clean.
		return err
	}

	s.logger.Info("http server stopped")
	return nil
}

// DecodeJSON reads a JSON body, rejecting unknown fields.
//
// Strictness is deliberate: a typo in an API client's payload should be an
// error the developer sees immediately, not a field silently ignored that they
// discover is missing weeks later in production.
func DecodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	// A second value in the stream means the client sent two documents.
	if err := decoder.Decode(new(struct{})); err == nil {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}
