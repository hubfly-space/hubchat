// Package telemetry connects Hubchat's existing observability surfaces to
// optional external sinks. The integration is deliberately failure-isolated:
// no DevLite network request is made on an application request goroutine.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"time"

	devlite "github.com/Ishimwe-Kevin/devlite-go"

	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/jobs"
)

const maxQueuedEvents = 4096

// Client owns the DevLite SDK and the periodic runtime/database metrics loop.
// A disabled Client remains safe to call, which keeps telemetry optional at
// every call site rather than spreading key checks through the application.
type Client struct {
	sdk      *devlite.Client
	base     *slog.Logger
	stop     chan struct{}
	stopOnce sync.Once
	metrics  sync.WaitGroup
}

// New initializes DevLite when an API key is configured. MaxBatchSize is one
// larger than the bounded queue so the SDK never reaches its synchronous
// threshold from Enqueue; only its background flusher performs network I/O.
func New(cfg config.DevLite, base *slog.Logger, version, commit string) (*Client, error) {
	client := &Client{base: base}
	if cfg.APIKey == "" {
		return client, nil
	}

	release := cfg.Release
	if release == "" {
		release = version
	}
	options := []devlite.Option{
		devlite.WithAPIKey(cfg.APIKey),
		devlite.WithEnvironment(cfg.Environment),
		devlite.WithServiceName(cfg.ServiceName),
		devlite.WithRelease(release),
		devlite.WithSampleRate(cfg.SampleRate),
		devlite.WithCaptureBody(false),
		devlite.WithScrubSensitiveData(true),
		devlite.WithCaptureSourceContext(true),
		devlite.WithFlushIntervalMs(int(cfg.FlushInterval.Milliseconds())),
		devlite.WithMaxQueueSize(maxQueuedEvents),
		devlite.WithMaxBatchSize(maxQueuedEvents + 1),
		devlite.WithMaxRetries(1),
		devlite.WithRetryBaseDelayMs(250),
		devlite.WithRequestTimeoutMs(2000),
		devlite.WithGzip(true),
		devlite.WithOnError(func(err error) {
			if base != nil {
				// Use the unwrapped logger so a delivery failure cannot recursively
				// create another DevLite event.
				base.Warn("devlite delivery failed", slog.Any("error", err))
			}
		}),
	}
	if cfg.Endpoint != "" {
		options = append(options, devlite.WithEndpoint(cfg.Endpoint))
	}

	sdk, err := devlite.NewClient(options...)
	if err != nil {
		return client, err
	}
	client.sdk = sdk
	client.stop = make(chan struct{})
	client.sdk.ReportDeployment(release, commit, "hubchat process started")
	return client, nil
}

// Enabled reports whether outbound telemetry is configured.
func (c *Client) Enabled() bool { return c != nil && c.sdk != nil }

// Logger returns a slog.Logger that preserves the configured stdout handler
// and mirrors every accepted structured record to DevLite. Error-valued attrs
// additionally create a grouped error event with source context.
func (c *Client) Logger(base slog.Handler) *slog.Logger {
	if !c.Enabled() {
		return slog.New(base)
	}
	return slog.New(&handler{next: base, client: c})
}

// Middleware adds DevLite's request timing and slow-request capture. Hubchat's
// outer recovery boundary mirrors panics as grouped error logs with the
// original stack while preserving WebSocket ResponseWriter capabilities.
// Request bodies and headers remain disabled; Hubchat support payloads can
// contain customer conversations and must never become telemetry.
func (c *Client) Middleware(next http.Handler) http.Handler {
	if !c.Enabled() {
		return next
	}
	return c.sdk.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestID := httpserver.RequestIDFrom(r.Context()); requestID != "" {
			c.sdk.SetTag("request_id", requestID)
		}
		next.ServeHTTP(w, r)
	}))
}

// Identify links errors to a stable local actor without transmitting names,
// email addresses, session tokens, API key material, or customer content.
func (c *Client) Identify(ctx context.Context, id, workspaceID, kind string) {
	if !c.Enabled() || id == "" {
		return
	}
	c.sdk.SetUserCtx(ctx, map[string]any{
		"id": id, "workspace_id": workspaceID, "kind": kind,
	})
}

// StartMetrics emits bounded process, PostgreSQL pool, and job-queue gauges.
// It reports immediately, then on the configured interval until shutdown.
func (c *Client) StartMetrics(ctx context.Context, interval time.Duration, pool *database.Pool, queue *jobs.Client) {
	if !c.Enabled() || interval <= 0 {
		return
	}
	c.metrics.Add(1)
	go func() {
		defer c.metrics.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			c.reportMetrics(ctx, pool, queue)
			select {
			case <-ctx.Done():
				return
			case <-c.stop:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (c *Client) reportMetrics(ctx context.Context, pool *database.Pool, queue *jobs.Client) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	c.sdk.ReportMetric("hubchat.runtime.goroutines", float64(runtime.NumGoroutine()), "count", nil)
	c.sdk.ReportMetric("hubchat.runtime.heap_alloc", float64(memory.HeapAlloc), "byte", nil)
	c.sdk.ReportMetric("hubchat.runtime.heap_inuse", float64(memory.HeapInuse), "byte", nil)
	c.sdk.ReportMetric("hubchat.runtime.gc_cycles", float64(memory.NumGC), "count", nil)

	if pool != nil {
		stats := pool.Stat()
		c.sdk.ReportMetric("hubchat.database.connections.total", float64(stats.TotalConns()), "count", nil)
		c.sdk.ReportMetric("hubchat.database.connections.acquired", float64(stats.AcquiredConns()), "count", nil)
		c.sdk.ReportMetric("hubchat.database.connections.idle", float64(stats.IdleConns()), "count", nil)
		c.sdk.ReportMetric("hubchat.database.connections.max", float64(stats.MaxConns()), "count", nil)
	}

	if queue == nil {
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	depths, err := queue.QueueDepth(checkCtx)
	if err != nil {
		c.sdk.CaptureError(err, map[string]any{"operation": "jobs.queue_depth"})
		return
	}
	total := 0
	for name, depth := range depths {
		total += depth
		c.sdk.ReportMetric("hubchat.jobs.queue_depth", float64(depth), "count", map[string]string{"queue": name})
	}
	c.sdk.ReportMetric("hubchat.jobs.queue_depth.total", float64(total), "count", nil)
}

// Close stops metric collection and flushes all queued events. The SDK's
// request timeout bounds the final flush so telemetry cannot hang shutdown.
func (c *Client) Close() {
	if !c.Enabled() {
		return
	}
	c.stopOnce.Do(func() { close(c.stop) })
	c.metrics.Wait()
	c.sdk.Close()
}

type handler struct {
	next   slog.Handler
	client *Client
	attrs  []slog.Attr
	groups []string
}

func (h *handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	err := h.next.Handle(ctx, record)
	fields := map[string]any{}
	var captured error
	for _, attr := range h.attrs {
		addAttr(fields, h.groups, attr, &captured)
	}
	record.Attrs(func(attr slog.Attr) bool {
		addAttr(fields, h.groups, attr, &captured)
		return true
	})
	h.client.sdk.CaptureLog(record.Level.String(), record.Message, fields)
	if captured != nil && record.Level >= slog.LevelError {
		h.client.sdk.CaptureErrorCtx(ctx, captured, fields)
	}
	return err
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := *h
	cloned.next = h.next.WithAttrs(attrs)
	cloned.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &cloned
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	cloned := *h
	cloned.next = h.next.WithGroup(name)
	cloned.groups = append(append([]string(nil), h.groups...), name)
	return &cloned
}

func addAttr(fields map[string]any, groups []string, attr slog.Attr, captured *error) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	target := fields
	for _, group := range groups {
		nested, ok := target[group].(map[string]any)
		if !ok {
			nested = map[string]any{}
			target[group] = nested
		}
		target = nested
	}
	if attr.Value.Kind() == slog.KindGroup {
		for _, child := range attr.Value.Group() {
			addAttr(target, []string{attr.Key}, child, captured)
		}
		return
	}
	value := attr.Value.Any()
	if eventErr, ok := value.(error); ok && *captured == nil {
		*captured = eventErr
	}
	target[attr.Key] = scrubSafeValue(value, 0)
}

// scrubSafeValue converts every structured slog value into JSON-shaped data
// before handing it to the SDK. DevLite v0.1.3 scrubs ordinary strings and
// map[string]any correctly, but error objects and typed maps otherwise pass
// through reflection without applying string/key redaction.
func scrubSafeValue(value any, depth int) any {
	if value == nil {
		return nil
	}
	if depth > 8 {
		return fmt.Sprint(value)
	}
	if err, ok := value.(error); ok {
		return err.Error()
	}
	if stringer, ok := value.(fmt.Stringer); ok {
		return stringer.String()
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String:
		return value
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return scrubSafeValue(rv.Elem().Interface(), depth+1)
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil
		}
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			out[i] = scrubSafeValue(rv.Index(i).Interface(), depth+1)
		}
		return out
	case reflect.Map:
		if rv.IsNil() {
			return nil
		}
		out := make(map[string]any, rv.Len())
		iterator := rv.MapRange()
		for iterator.Next() {
			out[fmt.Sprint(iterator.Key().Interface())] = scrubSafeValue(iterator.Value().Interface(), depth+1)
		}
		return out
	default:
		return fmt.Sprint(value)
	}
}
