package telemetry

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/httpserver"
)

func TestClientForwardsRequestsLogsErrorsAndScrubsSecrets(t *testing.T) {
	var mu sync.Mutex
	var events []map[string]any
	ingest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader := io.Reader(r.Body)
		if r.Header.Get("Content-Encoding") == "gzip" {
			compressed, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Errorf("open gzip payload: %v", err)
				http.Error(w, "bad gzip", http.StatusBadRequest)
				return
			}
			defer compressed.Close()
			reader = compressed
		}
		var payload struct {
			Events []map[string]any `json:"events"`
		}
		if err := json.NewDecoder(reader).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		mu.Lock()
		events = append(events, payload.Events...)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ingest.Close()

	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := New(config.DevLite{
		APIKey: "test-key", Endpoint: ingest.URL, Environment: "test",
		ServiceName: "hubchat-test", SampleRate: 1,
		FlushInterval: time.Hour, MetricsInterval: time.Hour,
	}, base, "v-test", "abc123")
	if err != nil {
		t.Fatal(err)
	}

	logger := client.Logger(base.Handler())
	handler := httpserver.RequestID(client.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client.Identify(r.Context(), "usr_test", "wrk_test", "agent")
		logger.ErrorContext(r.Context(), "operation failed",
			slog.Any("error", errors.New("lookup failed for user@example.test")),
			slog.String("token", "super-secret-token"),
		)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/test?secret=ignored", strings.NewReader("customer conversation"))
	request.Header.Set("Authorization", "Bearer must-not-leak")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	client.Close()

	mu.Lock()
	defer mu.Unlock()
	seen := map[string]bool{}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if kind, ok := event["type"].(string); ok {
			seen[kind] = true
		}
	}
	for _, kind := range []string{"deployment", "request", "log", "error"} {
		if !seen[kind] {
			t.Errorf("missing %s event in %#v", kind, events)
		}
	}
	contents := string(encoded)
	for _, secret := range []string{"user@example.test", "super-secret-token", "must-not-leak", "customer conversation", "secret=ignored"} {
		if strings.Contains(contents, secret) {
			t.Errorf("telemetry payload leaked %q: %s", secret, contents)
		}
	}
	if !strings.Contains(contents, "[REDACTED") {
		t.Errorf("expected scrubbed markers in telemetry payload: %s", contents)
	}
}

func TestDisabledClientIsNoOp(t *testing.T) {
	client, err := New(config.DevLite{}, slog.Default(), "dev", "none")
	if err != nil {
		t.Fatal(err)
	}
	if client.Enabled() {
		t.Fatal("client enabled without an API key")
	}
	client.Identify(context.Background(), "usr", "wrk", "agent")
	client.StartMetrics(context.Background(), time.Millisecond, nil, nil)
	client.Close()
}
