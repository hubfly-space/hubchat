//go:build integration

package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/widget"
)

func TestIdempotencyUsesWorkspacePathForPublicRetries(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email) VALUES ('usr_idempotency', 'Idempotency Test', 'idempotency@example.com');
		INSERT INTO workspaces (id, name, slug) VALUES ('wrk_idempotency', 'Idempotency Test', 'idempotency-test')
	`); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	handler := Idempotency(Deps{Pool: pool, Logger: slog.Default()})(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"created":true}`)
	})

	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/wrk_idempotency/contact/submissions", strings.NewReader(`{"name":"one"}`))
		r.Header.Set("Idempotency-Key", "public-retry-key")
		r.SetPathValue("workspaceID", "wrk_idempotency")
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}

	first := request()
	second := request()
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("unexpected statuses: first=%d second=%d", first.Code, second.Code)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want one", calls.Load())
	}
	if second.Header().Get("Idempotent-Replay") != "true" {
		t.Fatal("public retry was not marked as an idempotent replay")
	}
}

func TestUserIdempotencyProtectsWorkspaceBootstrap(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash) VALUES ('usr_bootstrap_idempotency', 'Bootstrap Test', 'bootstrap-idempotency@example.com', '')
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_sessions (id, user_id, token_hash, expires_at)
		VALUES ('ses_bootstrap_idempotency', 'usr_bootstrap_idempotency', $1, now() + interval '1 hour')
	`, auth.HashToken("bootstrap-session")); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	deps := Deps{
		Pool:   pool,
		Auth:   auth.New(pool, auth.Options{SessionLifetime: time.Hour}),
		Logger: slog.Default(),
	}
	handler := UserIdempotency(deps)(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"created":true}`)
	})

	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", strings.NewReader(`{"name":"Acme"}`))
		r.Header.Set("Idempotency-Key", "bootstrap-retry-key")
		r.AddCookie(&http.Cookie{Name: httpserver.SessionCookieName, Value: "bootstrap-session"})
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}

	first := request()
	second := request()
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("unexpected statuses: first=%d second=%d", first.Code, second.Code)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want one", calls.Load())
	}
	if second.Header().Get("Idempotent-Replay") != "true" {
		t.Fatal("workspace bootstrap retry was not marked as an idempotent replay")
	}
}

func TestWidgetIdempotencyScopesPublicRetriesToVisitor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	for _, statement := range []string{
		`INSERT INTO workspaces (id, name, slug) VALUES ('wrk_widget_idempotency', 'Widget Test', 'widget-idempotency')`,
		`INSERT INTO widgets (id, workspace_id, name, public_key) VALUES ('wdg_idempotency', 'wrk_widget_idempotency', 'Widget Test', 'pk_idempotency')`,
		`INSERT INTO widget_domains (id, widget_id, domain) VALUES ('wdom_idempotency', 'wdg_idempotency', 'customer.example')`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	var calls atomic.Int32
	deps := Deps{
		Pool:   pool,
		Widget: widget.New(pool, nil, nil, nil, nil, nil, []byte("widget-idempotency-secret")),
		Logger: slog.Default(),
	}
	handler := WidgetIdempotency(deps)(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"created":true}`)
	})

	request := func(token string) *httptest.ResponseRecorder {
		body := `{"public_key":"pk_idempotency","url":"https://customer.example/help","token":"` + token + `","body":"Hello"}`
		r := httptest.NewRequest(http.MethodPost, "/api/v1/widget/conversations", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", "widget-retry-key")
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}

	first := request("visitor-one")
	second := request("visitor-one")
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("unexpected statuses: first=%d second=%d", first.Code, second.Code)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want one", calls.Load())
	}
	if second.Header().Get("Idempotent-Replay") != "true" {
		t.Fatal("widget retry was not marked as an idempotent replay")
	}

	otherVisitor := request("visitor-two")
	if otherVisitor.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("different visitor should get an independent attempt: status=%d calls=%d", otherVisitor.Code, calls.Load())
	}
}
