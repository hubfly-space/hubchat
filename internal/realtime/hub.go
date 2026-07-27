// Package realtime owns the WebSocket gateway.
//
// # Responsibilities
//
// Connection authorization, topic subscription, event fan-out,
// resume-from-sequence, and heartbeats.
//
// # Boundary
//
// The hub delivers what the event log recorded; it does not decide what
// happened. Every frame a client receives originated as a row in
// workspace_events, which is what makes resume possible: a client that missed
// events while disconnected asks for them by sequence, and the answer comes
// from the same place the live frames did.
//
// Outbound queues are bounded and slow clients are disconnected. An unbounded
// queue turns one stalled browser into a server-wide memory leak (§17).
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/hubchat/hubchat/internal/events"
)

// TopicWorkspace is the firehose: every event in the workspace.
//
// Agents subscribe to it because the inbox list has to react to conversations
// they are not currently looking at. Visitors must never hold it — a widget
// gets `conversation:<id>` for its own thread and nothing else.
const TopicWorkspace = "workspace"

// EventSource is the subset of the event log the hub reads. Narrowed to an
// interface so the hub cannot accidentally grow the ability to write.
type EventSource interface {
	Since(ctx context.Context, workspaceID string, afterSequence int64, limit int) ([]events.Record, error)
	LatestSequence(ctx context.Context, workspaceID string) (int64, error)
}

// Hub fans events out to connections, filtered by topic.
type Hub struct {
	logger *slog.Logger
	source EventSource

	mu      sync.RWMutex
	clients map[*client]struct{}
	// byWorkspace indexes clients for O(subscribers) broadcast rather than
	// O(all connections) — the difference matters once a deployment has
	// several workspaces, each with their own busy inbox.
	byWorkspace map[string]map[*client]struct{}
	// cursor tracks how far each workspace has been broadcast, so a NOTIFY
	// signal reads only what is new rather than re-reading from zero.
	cursor map[string]int64

	outboundQueueSize int
	writeTimeout      time.Duration
	pingInterval      time.Duration
}

// NewHub returns a Hub. Call SetSource before Subscribe or Serve so resume has
// something to read from.
func NewHub(logger *slog.Logger, outboundQueueSize int) *Hub {
	if outboundQueueSize <= 0 {
		outboundQueueSize = 256
	}
	return &Hub{
		logger:            logger,
		clients:           make(map[*client]struct{}),
		byWorkspace:       make(map[string]map[*client]struct{}),
		cursor:            make(map[string]int64),
		outboundQueueSize: outboundQueueSize,
		writeTimeout:      5 * time.Second,
		pingInterval:      25 * time.Second,
	}
}

// Subscribe starts delivering events signalled on signals until ctx ends.
//
// The hub reads rows rather than trusting the signal to carry them: the
// payload only says "there is something new up to sequence N" (see
// events.Listener for why). Reading from the log means a signal that arrives
// late, out of order, or not at all still converges on the right answer.
func (h *Hub) Subscribe(ctx context.Context, signals <-chan events.Signal, source EventSource) {
	h.SetSource(source)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case signal, ok := <-signals:
				if !ok {
					return
				}
				h.drain(ctx, signal.WorkspaceID)
			}
		}
	}()
}

// SetSource wires the event log in. Separate from NewHub because the hub is
// constructed before the log in cmd/hubchat's dependency order.
func (h *Hub) SetSource(source EventSource) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.source = source
}

func (h *Hub) eventSource() EventSource {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.source
}

// drain reads everything new for a workspace and fans it out.
//
// It loops because one signal can cover more events than a single page holds —
// a bulk action or an import produces hundreds at once, and stopping after the
// first page would leave clients behind until the next unrelated write nudged
// them forward.
func (h *Hub) drain(ctx context.Context, workspaceID string) {
	source := h.eventSource()
	if source == nil {
		return
	}

	for {
		// Nobody is listening. Advancing the cursor anyway would mean the
		// first client to connect afterwards is told "you are up to date"
		// about events it never received — but that is correct, because a new
		// client starts from the current sequence by design and backfills
		// through the API, not the socket.
		if !h.hasClients(workspaceID) {
			h.advanceCursorToLatest(ctx, workspaceID, source)
			return
		}

		after := h.cursorFor(workspaceID)

		records, err := source.Since(ctx, workspaceID, after, drainPageSize)
		if err != nil {
			if ctx.Err() == nil {
				h.logger.Error("realtime: reading events failed",
					slog.String("workspace_id", workspaceID), slog.Any("error", err))
			}
			return
		}
		if len(records) == 0 {
			return
		}

		for _, record := range records {
			h.deliver(workspaceID, record)
		}

		h.setCursor(workspaceID, records[len(records)-1].Sequence)

		if len(records) < drainPageSize {
			return
		}
	}
}

// drainPageSize bounds one read. Large enough that a busy workspace drains in
// few round trips, small enough that a backlog does not materialise in memory
// all at once.
const drainPageSize = 200

func (h *Hub) hasClients(workspaceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byWorkspace[workspaceID]) > 0
}

func (h *Hub) cursorFor(workspaceID string) int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cursor[workspaceID]
}

func (h *Hub) setCursor(workspaceID string, sequence int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sequence > h.cursor[workspaceID] {
		h.cursor[workspaceID] = sequence
	}
}

func (h *Hub) advanceCursorToLatest(ctx context.Context, workspaceID string, source EventSource) {
	latest, err := source.LatestSequence(ctx, workspaceID)
	if err != nil {
		return
	}
	h.setCursor(workspaceID, latest)
}

// deliver routes one event to the clients whose topics match it.
func (h *Hub) deliver(workspaceID string, record events.Record) {
	payload, err := json.Marshal(record)
	if err != nil {
		h.logger.Error("realtime: encoding event failed",
			slog.String("event_id", record.ID), slog.Any("error", err))
		return
	}

	h.mu.RLock()
	targets := make([]*client, 0, len(h.byWorkspace[workspaceID]))
	for c := range h.byWorkspace[workspaceID] {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	topic := topicFor(record)
	for _, c := range targets {
		if !c.wants(topic) {
			continue
		}
		c.send(h, record.Sequence, payload)
	}
}

// topicFor names the narrow topic an event belongs to, if any. Events without
// an entity reach only workspace-wide subscribers.
func topicFor(record events.Record) string {
	if record.EntityType == "" || record.EntityID == "" {
		return ""
	}
	return record.EntityType + ":" + record.EntityID
}

// Serve upgrades the connection and blocks until it closes.
//
// workspaceID and the initial topic set have already been authorized by the
// caller: the HTTP handler resolves the session and workspace before calling
// this. Realtime does not re-derive authorization — it trusts the boundary
// that already checked, and never widens what that boundary granted.
func (h *Hub) Serve(ctx context.Context, conn *websocket.Conn, workspaceID string, grant Grant) {
	c := newClient(conn, workspaceID, grant, h.outboundQueueSize)

	h.register(c)
	defer h.unregister(c)

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	writerDone := make(chan struct{})
	go h.writeLoop(connCtx, c, writerDone)

	// A new client starts at the current head rather than at zero, so
	// connecting does not replay the workspace's entire history. A client that
	// held a position before disconnecting sends `resume` to get the gap.
	if source := h.eventSource(); source != nil {
		if latest, err := source.LatestSequence(connCtx, workspaceID); err == nil {
			c.setSequence(latest)
			h.setCursor(workspaceID, latest)
		}
	}

	c.sendControl(h, frameReady, map[string]any{
		"sequence": c.sequence(),
		"topics":   c.topicList(),
	})

	h.readLoop(connCtx, c)
	cancel()
	<-writerDone
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.clients[c] = struct{}{}
	if h.byWorkspace[c.workspaceID] == nil {
		h.byWorkspace[c.workspaceID] = make(map[*client]struct{})
	}
	h.byWorkspace[c.workspaceID][c] = struct{}{}
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.clients, c)
	if set, ok := h.byWorkspace[c.workspaceID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.byWorkspace, c.workspaceID)
			// Forget the cursor too. Keeping it would slowly accumulate one
			// entry per workspace that has ever connected, which on a large
			// deployment is a leak that never gets collected.
			delete(h.cursor, c.workspaceID)
		}
	}
}

// ConnectionCount reports total connected clients, for the readiness endpoint
// and operational metrics (§19).
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) writeLoop(ctx context.Context, c *client, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case payload, ok := <-c.outbound:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, h.writeTimeout)
			err := c.conn.Write(writeCtx, websocket.MessageText, payload)
			cancel()
			if err != nil {
				return
			}

		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, h.writeTimeout)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// readLoop handles client frames until the connection closes.
func (h *Hub) readLoop(ctx context.Context, c *client) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}

		var frame clientFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			c.sendControl(h, frameError, map[string]any{
				"code":    "malformed_frame",
				"message": "Frames must be JSON objects with an \"action\" field.",
			})
			continue
		}

		h.handleFrame(ctx, c, frame)
	}
}
