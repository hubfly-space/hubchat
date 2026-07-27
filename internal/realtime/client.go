package realtime

import (
	"encoding/json"
	"log/slog"
	"sort"
	"sync"

	"github.com/coder/websocket"

	"github.com/hubchat/hubchat/internal/events"
)

// Grant is what the HTTP layer authorized this connection to see.
//
// It is passed in rather than negotiated over the socket because a client must
// not be able to widen its own access by asking. §11.3 puts the boundary in
// the service layer; this type carries that decision into the hub, which then
// only ever narrows it.
type Grant struct {
	// Topics the connection may subscribe to. A connection holding
	// TopicWorkspace sees everything in the workspace.
	Allowed []string
	// Topics active immediately, so an agent's inbox is live without a
	// round trip.
	Initial []string
}

// AgentGrant is the grant for an authenticated member: the workspace firehose.
func AgentGrant() Grant {
	return Grant{
		Allowed: []string{TopicWorkspace},
		Initial: []string{TopicWorkspace},
	}
}

// VisitorGrant is the grant for a widget or portal visitor: exactly the
// conversations they own, and never the firehose.
func VisitorGrant(conversationIDs ...string) Grant {
	topics := make([]string, 0, len(conversationIDs))
	for _, id := range conversationIDs {
		topics = append(topics, "conversation:"+id)
	}
	return Grant{Allowed: topics, Initial: topics}
}

// client is one connected browser tab.
type client struct {
	conn        *websocket.Conn
	workspaceID string
	grant       Grant

	// outbound is bounded (§17): a client that cannot keep up is dropped
	// rather than allowed to accumulate an unbounded backlog in memory.
	outbound chan []byte

	mu       sync.Mutex
	topics   map[string]bool
	lastSeq  int64
	syncing  bool
	deferred []deferredEvent
}

// deferredEvent is a live event held back while a resume is in flight.
type deferredEvent struct {
	sequence int64
	payload  []byte
}

func newClient(conn *websocket.Conn, workspaceID string, grant Grant, queueSize int) *client {
	c := &client{
		conn:        conn,
		workspaceID: workspaceID,
		grant:       grant,
		outbound:    make(chan []byte, queueSize),
		topics:      make(map[string]bool, len(grant.Initial)),
	}
	for _, topic := range grant.Initial {
		if c.allows(topic) {
			c.topics[topic] = true
		}
	}
	return c
}

// allows reports whether the grant permits a topic. Called before every
// subscribe, so an unauthorized topic is refused rather than silently ignored.
func (c *client) allows(topic string) bool {
	for _, allowed := range c.grant.Allowed {
		if allowed == topic {
			return true
		}
	}
	return false
}

// wants reports whether this client should receive an event on the given
// narrow topic. Workspace subscribers receive everything; narrow subscribers
// receive only their own topic.
func (c *client) wants(topic string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.topics[TopicWorkspace] {
		return true
	}
	return topic != "" && c.topics[topic]
}

func (c *client) topicList() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	list := make([]string, 0, len(c.topics))
	for topic := range c.topics {
		list = append(list, topic)
	}
	sort.Strings(list)
	return list
}

func (c *client) sequence() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSeq
}

func (c *client) setSequence(sequence int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sequence > c.lastSeq {
		c.lastSeq = sequence
	}
}

// rewind moves the client's position backwards for a resume.
//
// Only resume may do this. Everywhere else the position is monotonic, because
// moving it backwards is how an event gets delivered twice.
func (c *client) rewind(sequence int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSeq = sequence
}

// advance claims a sequence for delivery, reporting false if this connection
// has already been sent it. The check and the update are one atomic step so
// two goroutines racing on the same event cannot both win.
func (c *client) advance(sequence int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if sequence <= c.lastSeq {
		return false
	}
	c.lastSeq = sequence
	return true
}

// send delivers one event payload, honouring resume ordering.
//
// Two things happen here that keep a client's view consistent:
//
//   - An event at or below the client's position is dropped. That is what
//     makes replay and live delivery safe to overlap: the same event arriving
//     from both paths is delivered once.
//   - While a resume is in flight, live events are held rather than sent.
//     Sending them immediately would deliver sequence 90 before the replayed
//     50–89, and a client that then recorded its position as 90 would never
//     ask for the gap again.
func (c *client) send(h *Hub, sequence int64, payload []byte) {
	c.mu.Lock()
	if sequence <= c.lastSeq {
		c.mu.Unlock()
		return
	}
	if c.syncing {
		c.deferred = append(c.deferred, deferredEvent{sequence: sequence, payload: payload})
		c.mu.Unlock()
		return
	}
	c.lastSeq = sequence
	c.mu.Unlock()

	c.enqueue(h, payload)
}

// enqueue pushes to the outbound queue, disconnecting a client that has fallen
// too far behind (§17 slow-client disconnection).
func (c *client) enqueue(h *Hub, payload []byte) {
	select {
	case c.outbound <- payload:
	default:
		h.logger.Warn("realtime: dropping slow client",
			slog.String("workspace_id", c.workspaceID))
		// Closing is enough: the write loop's next send fails, which unwinds
		// Serve and unregisters the client.
		_ = c.conn.Close(websocket.StatusPolicyViolation, "client too slow")
	}
}

// beginSync puts the client into resume mode, buffering live events.
func (c *client) beginSync() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.syncing = true
	c.deferred = nil
}

// endSync leaves resume mode and returns the buffered events still worth
// sending, in sequence order.
func (c *client) endSync() []deferredEvent {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.syncing = false
	pending := c.deferred
	c.deferred = nil

	sort.Slice(pending, func(i, j int) bool {
		return pending[i].sequence < pending[j].sequence
	})

	kept := pending[:0]
	for _, event := range pending {
		if event.sequence <= c.lastSeq {
			continue // already covered by the replay
		}
		c.lastSeq = event.sequence
		kept = append(kept, event)
	}
	return kept
}

// sendControl emits a protocol frame (ready, resumed, pong, error).
//
// Control frames carry no sequence and are exempt from the ordering rules in
// send: they describe the connection, not the workspace's history.
func (c *client) sendControl(h *Hub, frameType string, data any) {
	payload, err := json.Marshal(events.Record{
		Type: events.Type(frameType),
		Data: mustJSON(data),
	})
	if err != nil {
		h.logger.Error("realtime: encoding control frame failed",
			slog.String("frame", frameType), slog.Any("error", err))
		return
	}
	c.enqueue(h, payload)
}

func mustJSON(v any) json.RawMessage {
	payload, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return payload
}
