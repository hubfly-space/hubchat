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
	"strings"
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
	// viewers indexes clients currently subscribed to a "conversation:<id>"
	// topic, keyed by that topic string — §6.12's collision-prevention
	// presence. Only conversation topics are tracked here; TopicWorkspace
	// itself never needs a viewer list.
	viewers map[string]map[*client]struct{}
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
		viewers:           make(map[string]map[*client]struct{}),
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

// broadcastTyping relays a typing indicator live, to every other connection
// that would receive events on this conversation's topic — the same
// audience deliver would reach, computed the same way, but never durable:
// nothing here touches workspace_events or the per-workspace cursor.
//
// A visitor may only speak for the conversation its own grant names; an
// agent (holding the workspace firehose) always may, the same authority that
// already lets it see every conversation's durable events.
func (h *Hub) broadcastTyping(sender *client, conversationID string, typing bool) {
	if conversationID == "" {
		return
	}

	isAgent := sender.grant.MemberID != ""
	topic := "conversation:" + conversationID
	if !isAgent && !sender.allows(topic) {
		return
	}

	actorType := "customer"
	if isAgent {
		actorType = "agent"
	}
	payload, err := encodeFrame(framePresenceTyping, struct {
		ConversationID string `json:"conversation_id"`
		ActorType      string `json:"actor_type"`
		MemberID       string `json:"member_id,omitempty"`
		MemberName     string `json:"member_name,omitempty"`
		Typing         bool   `json:"typing"`
	}{
		ConversationID: conversationID,
		ActorType:      actorType, MemberID: sender.grant.MemberID, MemberName: sender.grant.MemberName,
		Typing: typing,
	})
	if err != nil {
		return
	}

	h.mu.RLock()
	targets := make([]*client, 0, len(h.byWorkspace[sender.workspaceID]))
	for c := range h.byWorkspace[sender.workspaceID] {
		if c != sender {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		if c.wants(topic) {
			c.enqueue(h, payload)
		}
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
	// Read before taking h.mu: c.topicList() takes the client's own lock, and
	// every other caller here locks h.mu first — matching that order avoids
	// a lock-ordering deadlock with, say, deliver holding h.mu while trying
	// to read a client's topics.
	topics := c.topicList()

	h.mu.Lock()
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
	var conversationTopics []string
	for _, topic := range topics {
		if strings.HasPrefix(topic, conversationTopicPrefix) {
			h.removeViewerLocked(topic, c)
			conversationTopics = append(conversationTopics, topic)
		}
	}
	h.mu.Unlock()

	// Broadcasting locks h.mu itself (RLock), so it happens after Unlock —
	// sync.RWMutex is not reentrant.
	for _, topic := range conversationTopics {
		h.broadcastViewers(c.workspaceID, topic)
	}
}

// addViewer and removeViewer maintain the per-conversation-topic viewer
// index that backs Viewers() and the "presence.viewing" broadcast — called
// from subscribeTopics/unsubscribeTopics for every topic that names a
// conversation (TopicWorkspace itself is never tracked here; everyone with
// it can already see everything, so "viewing" it means nothing).
func (h *Hub) addViewer(topic string, c *client) {
	if !strings.HasPrefix(topic, conversationTopicPrefix) {
		return
	}
	h.mu.Lock()
	if h.viewers[topic] == nil {
		h.viewers[topic] = make(map[*client]struct{})
	}
	h.viewers[topic][c] = struct{}{}
	h.mu.Unlock()

	h.broadcastViewers(c.workspaceID, topic)
}

func (h *Hub) removeViewer(topic string, c *client) {
	if !strings.HasPrefix(topic, conversationTopicPrefix) {
		return
	}
	h.mu.Lock()
	h.removeViewerLocked(topic, c)
	h.mu.Unlock()

	h.broadcastViewers(c.workspaceID, topic)
}

func (h *Hub) removeViewerLocked(topic string, c *client) {
	set, ok := h.viewers[topic]
	if !ok {
		return
	}
	delete(set, c)
	if len(set) == 0 {
		delete(h.viewers, topic)
	}
}

const conversationTopicPrefix = "conversation:"

// Viewers returns the member ids of every agent currently subscribed to a
// conversation's topic — §6.12's "someone else has this open" indicator.
// Visitor connections never appear here: they have no MemberID, and a
// visitor is never "another viewer" of their own conversation.
func (h *Hub) Viewers(conversationID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.viewersLocked(conversationTopicPrefix + conversationID)
}

// viewersLocked assumes h.mu is already held (read or write).
func (h *Hub) viewersLocked(topic string) []string {
	set := h.viewers[topic]
	out := make([]string, 0, len(set))
	seen := make(map[string]bool, len(set))
	for c := range set {
		if c.grant.MemberID == "" || seen[c.grant.MemberID] {
			continue
		}
		seen[c.grant.MemberID] = true
		out = append(out, c.grant.MemberID)
	}
	return out
}

// broadcastViewers sends the live viewer list to every connection in
// workspaceID that wants this conversation's topic, whenever the list
// changes. Like typing, this is presence — never appended to
// workspace_events, since a viewer list five minutes stale describes nobody
// actually looking anymore. Scoped to workspaceID explicitly (rather than
// matching topic across every connected client) because two different
// workspaces' agents both hold the bare TopicWorkspace membership that
// c.wants uses to mean "everything" — without the scope, one workspace's
// viewer presence would leak into another's.
func (h *Hub) broadcastViewers(workspaceID, topic string) {
	conversationID := strings.TrimPrefix(topic, conversationTopicPrefix)

	h.mu.RLock()
	viewers := h.viewersLocked(topic)
	targets := make([]*client, 0, len(h.byWorkspace[workspaceID]))
	for c := range h.byWorkspace[workspaceID] {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	payload, err := encodeFrame(framePresenceViewers, struct {
		ConversationID string   `json:"conversation_id"`
		Viewers        []string `json:"viewers"`
	}{ConversationID: conversationID, Viewers: viewers})
	if err != nil {
		return
	}

	for _, c := range targets {
		if c.wants(topic) {
			c.enqueue(h, payload)
		}
	}
}

// encodeFrame wraps data in the same events.Record envelope every durable
// event and control frame uses, so a client's one parser handles presence
// frames identically to everything else. Unlike client.sendControl (which
// sends to one connection) this returns the bytes so a broadcast to many
// targets encodes once rather than per recipient.
func encodeFrame(frameType string, data any) ([]byte, error) {
	return json.Marshal(events.Record{Type: events.Type(frameType), Data: mustJSON(data)})
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
