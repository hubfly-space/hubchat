package analytics

import (
	"encoding/json"
	"testing"

	"github.com/hubchat/hubchat/internal/events"
)

func TestMetricForEventUsesStableDefinitions(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"channel": "widget"})
	metric, dimensions, ok := metricForEvent(events.Record{Type: events.ConversationCreated, Data: data})
	if !ok || metric != "conversations.created" || dimensions["channel"] != "widget" {
		t.Fatalf("got metric=%q dimensions=%v ok=%v", metric, dimensions, ok)
	}

	data, _ = json.Marshal(map[string]string{"author_type": "customer"})
	metric, _, ok = metricForEvent(events.Record{Type: events.MessageCreated, Data: data})
	if !ok || metric != "messages.received" {
		t.Fatalf("customer message metric = %q, ok=%v", metric, ok)
	}
	if _, _, ok := metricForEvent(events.Record{Type: events.PresenceUpdate}); ok {
		t.Fatal("non-reporting event was folded")
	}
}
