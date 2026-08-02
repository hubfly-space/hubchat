package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hubchat/hubchat/embedded"
	"github.com/hubchat/hubchat/internal/config"
)

func TestProductionRouterServesHealthAndEmbeddedSurfaces(t *testing.T) {
	cfg := config.Default()
	cfg.Server.PublicURL, _ = url.Parse("http://localhost:8080")
	dashboard, err := embedded.Dashboard()
	if err != nil {
		t.Fatalf("load dashboard assets: %v", err)
	}
	portal, err := embedded.Portal()
	if err != nil {
		t.Fatalf("load portal assets: %v", err)
	}
	widget, err := embedded.Widget()
	if err != nil {
		t.Fatalf("load widget assets: %v", err)
	}
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), Assets{
		Dashboard: dashboard,
		Portal:    portal,
		Widget:    widget,
	}, Routes{})
	if err != nil {
		t.Fatalf("build production router: %v", err)
	}

	tests := []struct {
		path       string
		status     int
		bodyHas    string
		location   string
		contentKey string
		cache      string
	}{
		{path: "/healthz", status: http.StatusOK, bodyHas: `"status":"ok"`},
		{path: "/readyz", status: http.StatusOK, bodyHas: `"database":"not_configured"`},
		{path: "/app/", status: http.StatusOK, bodyHas: "<html"},
		{path: "/portal/", status: http.StatusOK, bodyHas: "<html"},
		{path: "/widget/app.js", status: http.StatusOK, contentKey: "javascript", cache: "public, max-age=0, must-revalidate"},
		{path: "/widget/app.css", status: http.StatusOK, contentKey: "text/css", cache: "public, max-age=0, must-revalidate"},
		{path: "/api/v1/meta", status: http.StatusServiceUnavailable, bodyHas: "API is not available"},
		{path: "/", status: http.StatusFound, location: "/app/"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			server.http.Handler.ServeHTTP(response, req)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.status, response.Body.String())
			}
			if test.bodyHas != "" && !strings.Contains(response.Body.String(), test.bodyHas) {
				t.Fatalf("body does not contain %q: %s", test.bodyHas, response.Body.String())
			}
			if test.location != "" && response.Header().Get("Location") != test.location {
				t.Fatalf("Location = %q, want %q", response.Header().Get("Location"), test.location)
			}
			if test.contentKey != "" && !strings.Contains(response.Header().Get("Content-Type"), test.contentKey) {
				t.Fatalf("Content-Type = %q, want it to contain %q", response.Header().Get("Content-Type"), test.contentKey)
			}
			if test.cache != "" && response.Header().Get("Cache-Control") != test.cache {
				t.Fatalf("Cache-Control = %q, want %q", response.Header().Get("Cache-Control"), test.cache)
			}
		})
	}
}

func TestProductionRouterHandlesConcurrentHealthAndAssetRequests(t *testing.T) {
	cfg := config.Default()
	cfg.Server.PublicURL, _ = url.Parse("http://localhost:8080")
	dashboard, err := embedded.Dashboard()
	if err != nil {
		t.Fatalf("load dashboard assets: %v", err)
	}
	portal, err := embedded.Portal()
	if err != nil {
		t.Fatalf("load portal assets: %v", err)
	}
	widget, err := embedded.Widget()
	if err != nil {
		t.Fatalf("load widget assets: %v", err)
	}
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), Assets{
		Dashboard: dashboard,
		Portal:    portal,
		Widget:    widget,
	}, Routes{})
	if err != nil {
		t.Fatalf("build production router: %v", err)
	}

	const workers = 16
	const requestsPerWorker = 25
	errorsCh := make(chan string, workers*requestsPerWorker)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for request := 0; request < requestsPerWorker; request++ {
				path := "/healthz"
				want := http.StatusOK
				if request%2 == 1 {
					path = "/widget/app.js"
				}
				response := httptest.NewRecorder()
				server.http.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != want {
					errorsCh <- path + " returned status " + strconv.Itoa(response.Code)
				}
			}
		}()
	}
	group.Wait()
	close(errorsCh)
	for message := range errorsCh {
		t.Error(message)
	}
}

func TestServerStartsServesHealthAndShutsDownCleanly(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test port: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	cfg := config.Default()
	cfg.Server.Listen = address
	cfg.Server.PublicURL, _ = url.Parse("http://" + address)
	cfg.Server.ShutdownTimeout = 2 * time.Second
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), Assets{}, Routes{})
	if err != nil {
		t.Fatalf("build lifecycle server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- server.Start(ctx) }()

	client := &http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		response, requestErr := client.Get("http://" + address + "/healthz")
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("health status = %d, want 200", response.StatusCode)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if time.Now().After(deadline) {
		t.Fatal("server never became ready on its configured listen address")
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server shutdown returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down after context cancellation")
	}
}
