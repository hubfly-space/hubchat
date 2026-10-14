package realtime

import (
	"encoding/json"
	"testing"

	"github.com/hubchat/hubchat/internal/events"
)

func TestBoundaryFilterProjectsAndDeniesEvents(t *testing.T) {
	grant := AgentGrant("member", "Agent")
	grant.Filter = func(record events.Record) (events.Record, bool) {
		if record.EntityType != "conversation" {
			return record, false
		}
		record.Data = json.RawMessage(`{"safe":true}`)
		return record, true
	}
	client := newClient(nil, "workspace", grant, 10)
	if _, ok := eventFor(client, events.Record{EntityType: "customer", Data: json.RawMessage(`{"secret":true}`)}); ok {
		t.Fatal("unauthorized entity visible")
	}
	projected, ok := eventFor(client, events.Record{EntityType: "conversation", Data: json.RawMessage(`{"secret":true}`)})
	if !ok || string(projected.Data) != `{"safe":true}` {
		t.Fatal("event projection ignored")
	}
	grant.AllowTopic = func(topic string) bool { return topic == TopicWorkspace || topic == "conversation:allowed" }
	client = newClient(nil, "workspace", grant, 10)
	if client.allows("ticket:restricted") {
		t.Fatal("workspace topic widened boundary")
	}
}
