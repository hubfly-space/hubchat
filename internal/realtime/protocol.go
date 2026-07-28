package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Frame types the server sends that are not workspace events. They share the
// event envelope's shape so a client has one parser, and are distinguished by
// the "hub." prefix, which no domain event may use.
const (
	frameReady   = "hub.ready"
	frameResumed = "hub.resumed"
	framePong    = "hub.pong"
	frameError   = "hub.error"
	frameTopics  = "hub.topics"
)

// clientFrame is anything the browser sends.
//
// The protocol is small on purpose. A socket that accepts arbitrary commands
// is a second API surface with its own authorization story; this one can
// subscribe within an existing grant, ask for missed events, and say hello.
// Everything that changes state goes over HTTP, where idempotency, rate
// limiting, and audit already apply.
type clientFrame struct {
	Action string   `json:"action"`
	Topics []string `json:"topics,omitempty"`
	// AfterSequence is the client's last known position for a resume.
	AfterSequence int64 `json:"after_sequence,omitempty"`
	// ConversationID and Typing carry a typing indicator (action "typing").
	ConversationID string `json:"conversation_id,omitempty"`
	Typing         bool   `json:"typing,omitempty"`
}

const (
	actionSubscribe   = "subscribe"
	actionUnsubscribe = "unsubscribe"
	actionResume      = "resume"
	actionPing        = "ping"
	actionTyping      = "typing"
)

// framePresenceTyping is the ephemeral frame type a typing indicator arrives
// as. Ephemeral — never appended to workspace_events — because "so-and-so is
// typing" has no meaning a second after it stops being true; recording it
// durably would mean resume replaying stale typing state on reconnect.
const framePresenceTyping = "presence.typing"

// framePresenceViewers is the ephemeral frame reporting who currently has a
// conversation open — recomputed and re-sent whenever a viewer subscribes,
// unsubscribes, or disconnects (see Hub.broadcastViewers). Never durable for
// the same reason typing is not: a viewer list is a description of this
// instant's connections, not a fact about what happened.
const framePresenceViewers = "presence.viewers"

func (h *Hub) handleFrame(ctx context.Context, c *client, frame clientFrame) {
	switch frame.Action {
	case actionPing:
		// An application-level ping, distinct from the protocol-level one the
		// write loop sends. Browsers cannot observe WebSocket pongs, so a
		// client that wants to prove liveness for itself needs this.
		c.sendControl(h, framePong, map[string]any{"sequence": c.sequence()})

	case actionSubscribe:
		h.subscribeTopics(c, frame.Topics)

	case actionUnsubscribe:
		h.unsubscribeTopics(c, frame.Topics)

	case actionResume:
		h.resume(ctx, c, frame.AfterSequence)

	case actionTyping:
		h.broadcastTyping(c, frame.ConversationID, frame.Typing)

	default:
		c.sendControl(h, frameError, map[string]any{
			"code":    "unknown_action",
			"message": "Unsupported action: " + frame.Action,
		})
	}
}

// subscribeTopics adds topics the grant permits and reports any it refused.
//
// Refusing loudly rather than silently ignoring matters: a widget that asks
// for the workspace firehose has a bug (or is probing), and a client that
// believes it is subscribed to something it is not will sit waiting for events
// that never come.
func (h *Hub) subscribeTopics(c *client, topics []string) {
	var refused, added []string

	c.mu.Lock()
	for _, topic := range topics {
		if !c.allows(topic) {
			refused = append(refused, topic)
			continue
		}
		if !c.topics[topic] {
			added = append(added, topic)
		}
		c.topics[topic] = true
	}
	c.mu.Unlock()

	if len(refused) > 0 {
		h.logger.Warn("realtime: refused topic subscription",
			slog.String("workspace_id", c.workspaceID),
			slog.Any("topics", refused))
		c.sendControl(h, frameError, map[string]any{
			"code":    "topic_forbidden",
			"message": "This connection may not subscribe to those topics.",
			"topics":  refused,
		})
	}

	// Subscribing to a conversation topic is also how a client says "I have
	// this open" — see Hub.addViewer.
	for _, topic := range added {
		h.addViewer(topic, c)
	}

	c.sendControl(h, frameTopics, map[string]any{"topics": c.topicList()})
}

func (h *Hub) unsubscribeTopics(c *client, topics []string) {
	var removed []string

	c.mu.Lock()
	for _, topic := range topics {
		if c.topics[topic] {
			removed = append(removed, topic)
		}
		delete(c.topics, topic)
	}
	c.mu.Unlock()

	for _, topic := range removed {
		h.removeViewer(topic, c)
	}

	c.sendControl(h, frameTopics, map[string]any{"topics": c.topicList()})
}

// resume replays everything the client missed, then returns it to live
// delivery.
//
// The client is put into syncing mode first so that events arriving mid-replay
// are buffered rather than delivered out of order — see client.send. Without
// that, a busy workspace would interleave live events with replayed ones and
// the client's position would jump past the gap it was trying to close.
func (h *Hub) resume(ctx context.Context, c *client, afterSequence int64) {
	source := h.eventSource()
	if source == nil {
		c.sendControl(h, frameError, map[string]any{
			"code":    "resume_unavailable",
			"message": "This server cannot replay events.",
		})
		return
	}

	if afterSequence < 0 {
		afterSequence = 0
	}

	// The client is authoritative about its own position, so resume rewinds to
	// it rather than filtering against where this connection happens to be.
	//
	// That distinction is the whole point on a reconnect: Serve starts a fresh
	// connection at the log head so it does not replay history it never asked
	// for, but a reconnecting client's real position is far behind that head.
	// Filtering the replay against the head would drop precisely the events
	// the client reconnected to collect.
	c.beginSync()
	c.rewind(afterSequence)

	from := afterSequence
	delivered := 0
	truncated := false

	for {
		records, err := source.Since(ctx, c.workspaceID, from, resumePageSize)
		if err != nil {
			c.endSync()
			h.logger.Error("realtime: resume failed",
				slog.String("workspace_id", c.workspaceID), slog.Any("error", err))
			c.sendControl(h, frameError, map[string]any{
				"code":    "resume_failed",
				"message": "Missed events could not be replayed. Reload to resynchronise.",
			})
			return
		}
		if len(records) == 0 {
			break
		}

		for _, record := range records {
			if !c.wants(topicFor(record)) {
				continue
			}
			payload, err := json.Marshal(record)
			if err != nil {
				continue
			}
			// advance reports false when this connection has already been sent
			// the event, which is what keeps overlapping replay pages and
			// concurrent live traffic from double-delivering.
			if !c.advance(record.Sequence) {
				continue
			}
			c.enqueue(h, payload)
			delivered++
		}

		from = records[len(records)-1].Sequence

		if len(records) < resumePageSize {
			break
		}
		// A client that has been away long enough to exceed this is better
		// served by refetching through the API than by having the server
		// stream an unbounded backlog into a bounded queue.
		if delivered >= maxResumeEvents {
			truncated = true
			break
		}
	}

	for _, event := range c.endSync() {
		c.enqueue(h, event.payload)
	}

	c.sendControl(h, frameResumed, map[string]any{
		"from":      afterSequence,
		"sequence":  c.sequence(),
		"delivered": delivered,
		// When true the client missed more than the socket will replay and
		// must refetch its lists over HTTP. Saying so explicitly is what keeps
		// it from believing it is up to date when it is not.
		"truncated": truncated,
	})
}

const (
	resumePageSize  = 200
	maxResumeEvents = 2000
)
