// Package realtime owns the WebSocket gateway.
//
// # Responsibilities
//
// Connection authorization, subscription management, event fan-out,
// heartbeats, and resume-from-sequence.
//
// # Boundary
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
)

// Envelope is the one shape every realtime frame takes (docs/api-conventions.md
// §8), shared with the webhook body format so a consumer only learns one
// contract.
type Envelope struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	WorkspaceID string `json:"workspace_id"`
	OccurredAt  string `json:"occurred_at"`
	Sequence    int64  `json:"sequence"`
	Data        any    `json:"data"`
}

// client is one connected browser tab.
type client struct {
	conn        *websocket.Conn
	workspaceID string
	// outbound is bounded (§17): a client that cannot keep up is dropped
	// rather than allowed to accumulate an unbounded backlog in memory.
	outbound chan []byte
}

// Hub fans events out to every connection subscribed to a workspace.
type Hub struct {
	logger *slog.Logger

	mu      sync.RWMutex
	clients map[*client]struct{}
	// byWorkspace indexes clients for O(subscribers) broadcast rather than
	// O(all connections) — the difference matters once a deployment has
	// several workspaces, each with their own busy inbox.
	byWorkspace map[string]map[*client]struct{}

	outboundQueueSize int
	writeTimeout      time.Duration
	pingInterval      time.Duration
}

func NewHub(logger *slog.Logger, outboundQueueSize int) *Hub {
	if outboundQueueSize <= 0 {
		outboundQueueSize = 256
	}
	return &Hub{
		logger:            logger,
		clients:           make(map[*client]struct{}),
		byWorkspace:       make(map[string]map[*client]struct{}),
		outboundQueueSize: outboundQueueSize,
		writeTimeout:      5 * time.Second,
		pingInterval:      25 * time.Second,
	}
}

// Serve upgrades the connection and blocks until it closes. workspaceID has
// already been authorized by the caller (the HTTP handler resolves the
// session and workspace before calling this — realtime does not re-derive
// authorization, it trusts the boundary that already checked it).
func (h *Hub) Serve(ctx context.Context, conn *websocket.Conn, workspaceID string) {
	c := &client{
		conn:        conn,
		workspaceID: workspaceID,
		outbound:    make(chan []byte, h.outboundQueueSize),
	}

	h.register(c)
	defer h.unregister(c)

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	writerDone := make(chan struct{})
	go h.writeLoop(connCtx, c, writerDone)

	// The read loop's only job is to notice the connection closing. Clients
	// send no meaningful application data over this socket today (it is
	// server → browser push only); a future resume/subscribe protocol reads
	// here.
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
		}
	}
}

// broadcast sends payload to every client subscribed to workspaceID. A client
// whose outbound queue is full is disconnected rather than blocked on — the
// slow-client protection §17 requires.
func (h *Hub) broadcast(workspaceID string, payload []byte) {
	h.mu.RLock()
	targets := make([]*client, 0, len(h.byWorkspace[workspaceID]))
	for c := range h.byWorkspace[workspaceID] {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.outbound <- payload:
		default:
			h.logger.Warn("realtime: dropping slow client",
				slog.String("workspace_id", workspaceID))
			// Closing here is enough: the write loop's next send fails, which
			// unwinds Serve and unregisters the client.
			_ = c.conn.Close(websocket.StatusPolicyViolation, "client too slow")
		}
	}
}

// Publish encodes an event and fans it out. Called by module Broadcaster
// adapters (see BroadcastMessage) — never by a handler directly, so every
// realtime event goes through the same envelope shape.
func (h *Hub) Publish(workspaceID, eventType string, sequence int64, data any) {
	envelope := Envelope{
		ID:          eventID(),
		Type:        eventType,
		WorkspaceID: workspaceID,
		OccurredAt:  nowRFC3339(),
		Sequence:    sequence,
		Data:        data,
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		h.logger.Error("realtime: encode envelope failed", slog.Any("error", err))
		return
	}

	h.broadcast(workspaceID, payload)
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

func (h *Hub) readLoop(ctx context.Context, c *client) {
	for {
		_, _, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		// Inbound application messages are not part of the protocol yet;
		// received frames are discarded. Reading is still necessary so the
		// library processes control frames (ping/pong/close) and so a closed
		// connection is detected promptly.
	}
}
