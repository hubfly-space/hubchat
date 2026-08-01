//go:build integration

package realtime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/realtime"
)

// A connected agent receives events appended after they connected.
func TestHubDeliversLiveEvents(t *testing.T) {
	h := newHarness(t)
	conn := h.connect(t, realtime.AgentGrant("mem_test", "Test Agent"))

	h.expectFrame(t, conn, "hub.ready")

	h.append(t, events.Event{
		WorkspaceID: h.workspaceID,
		Type:        events.MessageCreated,
		EntityType:  "conversation",
		EntityID:    "cnv_live",
	})

	frame := h.expectFrame(t, conn, string(events.MessageCreated))
	if frame.Sequence == 0 {
		t.Fatal("delivered event carried no sequence; resume would be impossible")
	}
}

func TestHubFansOutAConcurrentInboxBurstWithoutGaps(t *testing.T) {
	h := newHarness(t)
	const clients = 6
	const burst = 80
	connections := make([]*websocket.Conn, 0, clients)
	for index := 0; index < clients; index++ {
		conn := h.connect(t, realtime.AgentGrant(fmt.Sprintf("mem_load_%d", index), "Load Agent"))
		h.expectFrame(t, conn, "hub.ready")
		connections = append(connections, conn)
	}

	for index := 0; index < burst; index++ {
		h.append(t, events.Event{
			WorkspaceID: h.workspaceID,
			Type:        events.MessageCreated,
			EntityType:  "conversation",
			EntityID:    fmt.Sprintf("cnv_load_%d", index),
		})
	}

	for clientIndex, conn := range connections {
		var previous int64
		for eventIndex := 0; eventIndex < burst; eventIndex++ {
			frame := h.readFrame(t, conn)
			if frame.Type != events.MessageCreated {
				t.Fatalf("client %d received frame type %q at event %d, want %q", clientIndex, frame.Type, eventIndex, events.MessageCreated)
			}
			if frame.Sequence <= previous {
				t.Fatalf("client %d received non-increasing sequence %d after %d", clientIndex, frame.Sequence, previous)
			}
			previous = frame.Sequence
		}
	}
}

// The core resume guarantee: events appended while a client was away are
// replayed on reconnect, in order, with none missing.
func TestResumeReplaysEventsMissedWhileDisconnected(t *testing.T) {
	h := newHarness(t)

	conn := h.connect(t, realtime.AgentGrant("mem_test", "Test Agent"))
	ready := h.expectFrame(t, conn, "hub.ready")
	position := ready.Sequence

	h.append(t, events.Event{WorkspaceID: h.workspaceID, Type: events.MessageCreated})
	h.expectFrame(t, conn, string(events.MessageCreated))
	position = h.latest(t)

	// Client goes away.
	conn.Close(websocket.StatusNormalClosure, "")

	// Three events happen without it.
	for range 3 {
		h.append(t, events.Event{
			WorkspaceID: h.workspaceID,
			Type:        events.MessageCreated,
			EntityType:  "conversation",
			EntityID:    "cnv_missed",
		})
	}

	// Client comes back and asks for the gap.
	reconnected := h.connect(t, realtime.AgentGrant("mem_test", "Test Agent"))
	h.expectFrame(t, reconnected, "hub.ready")
	h.send(t, reconnected, map[string]any{
		"action":         "resume",
		"after_sequence": position,
	})

	var replayed int
	for {
		frame := h.readFrame(t, reconnected)
		if frame.Type == "hub.resumed" {
			break
		}
		if frame.Type == events.MessageCreated {
			replayed++
		}
	}

	if replayed != 3 {
		t.Fatalf("resume replayed %d events, want 3 — a client would silently miss messages", replayed)
	}
}

// A reconnecting client must receive every missed event exactly once, even
// when new events keep arriving while the replay is in flight.
//
// This is the case the buffering in client.beginSync/endSync exists for:
// without it, a live event at sequence 12 would be written to the socket ahead
// of the replayed 5-11, and a client recording its position as 12 would never
// ask for the gap again.
func TestResumeIsExactlyOnceWhileLiveEventsArrive(t *testing.T) {
	h := newHarness(t)

	// Establish a position, then disconnect.
	first := h.connect(t, realtime.AgentGrant("mem_test", "Test Agent"))
	h.expectFrame(t, first, "hub.ready")
	h.append(t, events.Event{WorkspaceID: h.workspaceID, Type: events.MessageCreated})
	h.expectFrame(t, first, string(events.MessageCreated))
	position := h.latest(t)
	first.Close(websocket.StatusNormalClosure, "")

	// Events pile up while the client is away.
	const missed = 6
	for range missed {
		h.append(t, events.Event{WorkspaceID: h.workspaceID, Type: events.MessageCreated})
	}

	conn := h.connect(t, realtime.AgentGrant("mem_test", "Test Agent"))
	h.expectFrame(t, conn, "hub.ready")
	h.send(t, conn, map[string]any{"action": "resume", "after_sequence": position})

	// And keep arriving during the replay.
	const during = 4
	for range during {
		h.append(t, events.Event{WorkspaceID: h.workspaceID, Type: events.MessageCreated})
	}

	seen := make(map[int64]int)
	var resumed bool
	for len(seen) < missed+during || !resumed {
		frame := h.readFrame(t, conn)
		if frame.Type == "hub.resumed" {
			resumed = true
			continue
		}
		seen[frame.Sequence]++
	}

	for sequence, count := range seen {
		if count > 1 {
			t.Fatalf("sequence %d was delivered %d times; the client would render a duplicate message",
				sequence, count)
		}
	}

	// Every sequence after the client's position must be present, with no gap.
	for want := position + 1; want <= position+missed+during; want++ {
		if seen[want] == 0 {
			t.Fatalf("sequence %d was never delivered; the client silently missed a message", want)
		}
	}

	// And they must have arrived in order, or a client applying them in
	// receipt order would render the conversation wrong.
	if !resumed {
		t.Fatal("never received the hub.resumed acknowledgement")
	}
}

// §11.3: a visitor connection must never receive another conversation's
// traffic, even within the same workspace.
func TestVisitorGrantDoesNotLeakOtherConversations(t *testing.T) {
	h := newHarness(t)

	conn := h.connect(t, realtime.VisitorGrant("cnv_mine"))
	h.expectFrame(t, conn, "hub.ready")

	// An event on someone else's conversation.
	h.append(t, events.Event{
		WorkspaceID: h.workspaceID,
		Type:        events.MessageCreated,
		EntityType:  "conversation",
		EntityID:    "cnv_theirs",
	})
	// Then one on theirs, so the test has something to wait for rather than
	// proving a negative by timeout alone.
	h.append(t, events.Event{
		WorkspaceID: h.workspaceID,
		Type:        events.MessageCreated,
		EntityType:  "conversation",
		EntityID:    "cnv_mine",
	})

	frame := h.expectFrame(t, conn, string(events.MessageCreated))
	if frame.EntityID != "cnv_mine" {
		t.Fatalf("visitor received an event for %q; expected only cnv_mine", frame.EntityID)
	}
}

// A visitor asking for the workspace firehose must be refused, not quietly
// upgraded.
func TestSubscribeRefusesTopicsOutsideTheGrant(t *testing.T) {
	h := newHarness(t)
	conn := h.connect(t, realtime.VisitorGrant("cnv_mine"))
	h.expectFrame(t, conn, "hub.ready")

	h.send(t, conn, map[string]any{
		"action": "subscribe",
		"topics": []string{realtime.TopicWorkspace},
	})

	frame := h.expectFrame(t, conn, "hub.error")
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(frame.Data, &payload); err != nil {
		t.Fatalf("decode error frame: %v", err)
	}
	if payload.Code != "topic_forbidden" {
		t.Fatalf("got error code %q, want topic_forbidden", payload.Code)
	}

	// And the escalation must not have taken effect.
	h.append(t, events.Event{
		WorkspaceID: h.workspaceID,
		Type:        events.MessageCreated,
		EntityType:  "conversation",
		EntityID:    "cnv_theirs",
	})
	h.append(t, events.Event{
		WorkspaceID: h.workspaceID,
		Type:        events.MessageCreated,
		EntityType:  "conversation",
		EntityID:    "cnv_mine",
	})

	delivered := h.expectFrame(t, conn, string(events.MessageCreated))
	if delivered.EntityID != "cnv_mine" {
		t.Fatalf("refused subscription still leaked %q", delivered.EntityID)
	}
}

// Events belonging to another workspace must never reach this connection.
func TestHubIsScopedToOneWorkspace(t *testing.T) {
	h := newHarness(t)
	other := h.seedWorkspace(t, "other")

	conn := h.connect(t, realtime.AgentGrant("mem_test", "Test Agent"))
	h.expectFrame(t, conn, "hub.ready")

	h.append(t, events.Event{WorkspaceID: other, Type: events.TicketCreated})
	h.append(t, events.Event{WorkspaceID: h.workspaceID, Type: events.MessageCreated})

	frame := h.expectFrame(t, conn, string(events.MessageCreated))
	if frame.WorkspaceID != h.workspaceID {
		t.Fatalf("cross-tenant leak: received an event for workspace %q", frame.WorkspaceID)
	}
}

func TestPingIsAnswered(t *testing.T) {
	h := newHarness(t)
	conn := h.connect(t, realtime.AgentGrant("mem_test", "Test Agent"))
	h.expectFrame(t, conn, "hub.ready")

	h.send(t, conn, map[string]any{"action": "ping"})
	h.expectFrame(t, conn, "hub.pong")
}

func TestUnknownActionIsReported(t *testing.T) {
	h := newHarness(t)
	conn := h.connect(t, realtime.AgentGrant("mem_test", "Test Agent"))
	h.expectFrame(t, conn, "hub.ready")

	h.send(t, conn, map[string]any{"action": "drop_tables"})
	h.expectFrame(t, conn, "hub.error")
}

// ----------------------------------------------------------------- harness

type harness struct {
	pool        *database.Pool
	log         *events.Log
	hub         *realtime.Hub
	server      *httptest.Server
	workspaceID string
	signals     chan events.Signal
	// grants hands the next connection's authorization to the test server,
	// standing in for the session check the real handler performs.
	grants chan realtime.Grant
	ctx    context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	log := events.New(pool)
	hub := realtime.NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)), 64)

	h := &harness{
		pool:    pool,
		log:     log,
		hub:     hub,
		signals: make(chan events.Signal, 256),
		ctx:     ctx,
	}
	h.workspaceID = h.seedWorkspace(t, "primary")

	// Drive the hub from an in-process signal channel rather than real
	// LISTEN/NOTIFY. The hub's contract is "a signal arrives, read the log and
	// fan out"; where the signal came from is the listener's concern, tested
	// separately.
	hub.Subscribe(ctx, h.signals, log)

	grantMu := make(chan realtime.Grant, 1)
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		grant := <-grantMu
		hub.Serve(r.Context(), conn, h.workspaceID, grant)
	}))
	t.Cleanup(h.server.Close)
	h.grants = grantMu

	return h
}

func (h *harness) seedWorkspace(t *testing.T, slug string) string {
	t.Helper()

	id := ids.New(ids.PrefixWorkspace)
	if _, err := h.pool.Exec(h.ctx, `
		INSERT INTO workspaces (id, name, slug, ticket_prefix)
		VALUES ($1, $2, $3, 'SUP')
	`, id, slug, slug); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	return id
}

// append writes an event and signals the hub, mimicking what a service does
// inside its own transaction.
func (h *harness) append(t *testing.T, event events.Event) {
	t.Helper()

	tx, err := h.pool.Begin(h.ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(h.ctx)

	record, err := h.log.Append(h.ctx, tx, event)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(h.ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	h.signals <- events.Signal{WorkspaceID: event.WorkspaceID, Sequence: record.Sequence}
}

func (h *harness) connect(t *testing.T, grant realtime.Grant) *websocket.Conn {
	t.Helper()

	h.grants <- grant

	url := "ws" + h.server.URL[len("http"):]
	conn, _, err := websocket.Dial(h.ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func (h *harness) send(t *testing.T, conn *websocket.Conn, frame map[string]any) {
	t.Helper()

	payload, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if err := conn.Write(h.ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func (h *harness) readFrame(t *testing.T, conn *websocket.Conn) events.Record {
	t.Helper()

	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	var record events.Record
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode frame %q: %v", data, err)
	}
	return record
}

// expectFrame reads until a frame of the wanted type arrives.
func (h *harness) expectFrame(t *testing.T, conn *websocket.Conn, want string) events.Record {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		frame := h.readFrame(t, conn)
		if string(frame.Type) == want {
			return frame
		}
	}
	t.Fatalf("timed out waiting for a %q frame", want)
	return events.Record{}
}

// latest reports the workspace's current head, standing in for the position a
// real client would have persisted from the last frame it applied.
func (h *harness) latest(t *testing.T) int64 {
	t.Helper()

	sequence, err := h.log.LatestSequence(h.ctx, h.workspaceID)
	if err != nil {
		t.Fatalf("latest sequence: %v", err)
	}
	return sequence
}

func mustMarshal(record events.Record) []byte {
	payload, _ := json.Marshal(record)
	return payload
}

// A typing indicator from one agent reaches another agent in the same
// workspace, tagged with who is typing.
func TestTypingReachesOtherAgents(t *testing.T) {
	h := newHarness(t)

	sender := h.connect(t, realtime.AgentGrant("mem_sender", "Sender Agent"))
	h.expectFrame(t, sender, "hub.ready")

	receiver := h.connect(t, realtime.AgentGrant("mem_receiver", "Receiver Agent"))
	h.expectFrame(t, receiver, "hub.ready")

	h.send(t, sender, map[string]any{
		"action":          "typing",
		"conversation_id": "cnv_123",
		"typing":          true,
	})

	frame := h.expectFrame(t, receiver, "presence.typing")
	var payload struct {
		ConversationID string `json:"conversation_id"`
		MemberID       string `json:"member_id"`
		Typing         bool   `json:"typing"`
	}
	if err := json.Unmarshal(frame.Data, &payload); err != nil {
		t.Fatalf("decode presence.typing: %v", err)
	}
	if payload.ConversationID != "cnv_123" || payload.MemberID != "mem_sender" || !payload.Typing {
		t.Fatalf("unexpected typing payload: %+v", payload)
	}
}

// A visitor may only announce typing for the conversation its own grant
// names — announcing on any other id must be silently dropped, not just
// filtered on delivery.
func TestVisitorCannotAnnounceTypingForAnotherConversation(t *testing.T) {
	h := newHarness(t)

	agent := h.connect(t, realtime.AgentGrant("mem_agent", "Agent"))
	h.expectFrame(t, agent, "hub.ready")

	visitor := h.connect(t, realtime.VisitorGrant("cnv_mine"))
	h.expectFrame(t, visitor, "hub.ready")

	h.send(t, visitor, map[string]any{
		"action":          "typing",
		"conversation_id": "cnv_not_mine",
		"typing":          true,
	})

	// The legitimate one must still get through, so the test has a positive
	// signal to wait for rather than proving a negative by timeout alone.
	h.send(t, visitor, map[string]any{
		"action":          "typing",
		"conversation_id": "cnv_mine",
		"typing":          true,
	})

	frame := h.expectFrame(t, agent, "presence.typing")
	var payload struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal(frame.Data, &payload); err != nil {
		t.Fatalf("decode presence.typing: %v", err)
	}
	if payload.ConversationID != "cnv_mine" {
		t.Fatalf("visitor's typing for another conversation was not dropped: got %q", payload.ConversationID)
	}
}

// Subscribing to a conversation topic is also how an agent says "I have this
// open" — another agent subscribed to the same conversation sees the viewer
// list update live, and it updates again when the viewer disconnects.
func TestSubscribingToAConversationReportsAsAViewer(t *testing.T) {
	h := newHarness(t)

	viewer := h.connect(t, realtime.AgentGrant("mem_viewer", "Viewer"))
	h.expectFrame(t, viewer, "hub.ready")

	watcher := h.connect(t, realtime.AgentGrant("mem_watcher", "Watcher"))
	h.expectFrame(t, watcher, "hub.ready")
	h.send(t, watcher, map[string]any{
		"action": "subscribe",
		"topics": []string{"conversation:cnv_shared"},
	})
	// The watcher's own subscribe already produces a presence.viewers frame
	// (itself joining) interleaved with the hub.topics ack in either order;
	// expectFrame skips past whichever comes first, so this drains exactly
	// that one before the one caused by viewer joining below.
	h.expectFrame(t, watcher, "presence.viewers")

	h.send(t, viewer, map[string]any{
		"action": "subscribe",
		"topics": []string{"conversation:cnv_shared"},
	})

	frame := h.expectFrame(t, watcher, "presence.viewers")
	var payload struct {
		ConversationID string   `json:"conversation_id"`
		Viewers        []string `json:"viewers"`
	}
	if err := json.Unmarshal(frame.Data, &payload); err != nil {
		t.Fatalf("decode presence.viewers: %v", err)
	}
	if payload.ConversationID != "cnv_shared" {
		t.Fatalf("unexpected conversation id: %q", payload.ConversationID)
	}
	found := false
	for _, id := range payload.Viewers {
		if id == "mem_viewer" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mem_viewer in the viewer list, got %v", payload.Viewers)
	}
}
