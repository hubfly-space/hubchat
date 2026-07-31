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

func TestMetricForEventPromotesOnlyAllowlistedSurfaceEvents(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"type": "widget.impression"})
	metric, _, ok := metricForEvent(events.Record{Type: events.EventReceived, Data: data})
	if !ok || metric != "surfaces.widget.impressions" {
		t.Fatalf("widget impression metric = %q, ok=%v", metric, ok)
	}

	data, _ = json.Marshal(map[string]string{"type": "widget.article_viewed"})
	metric, _, ok = metricForEvent(events.Record{Type: events.EventReceived, Data: data})
	if !ok || metric != "surfaces.widget.articles_viewed" {
		t.Fatalf("widget article metric = %q, ok=%v", metric, ok)
	}

	data, _ = json.Marshal(map[string]string{"type": "checkout.started"})
	if _, _, ok := metricForEvent(events.Record{Type: events.EventReceived, Data: data}); ok {
		t.Fatal("arbitrary customer events must not become report metrics")
	}
}
