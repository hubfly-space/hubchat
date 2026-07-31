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

	"github.com/hubchat/hubchat/internal/database/dbtest"
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
