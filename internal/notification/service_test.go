package notification

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/hubchat/hubchat/internal/events"
)

type fakeSurveyDispatcher struct {
	status string
}

func (f *fakeSurveyDispatcher) NotifyTicketResolution(_ context.Context, _, _, _, status string) error {
	f.status = status
	return nil
}

func TestNormalizePreferences(t *testing.T) {
	items, err := normalizePreferences([]PreferenceInput{{Type: " Reply ", InApp: true}})
	if err != nil || len(items) != 1 || items[0].Type != "reply" || !items[0].InApp {
		t.Fatalf("normalized preferences = %+v, err=%v", items, err)
	}
	for _, input := range [][]PreferenceInput{
		{{Type: "unknown"}},
		{{Type: "reply"}, {Type: "reply"}},
	} {
		if _, err := normalizePreferences(input); !errors.Is(err, ErrInvalidPreference) {
			t.Fatalf("normalizePreferences(%+v) error = %v, want ErrInvalidPreference", input, err)
		}
	}
}

func TestSLANotificationMapping(t *testing.T) {
	preference, typ, title, body := slaNotification("approaching")
	if preference != "sla_warning" || typ != "sla_warning" || title == "" || body == "" {
		t.Fatalf("approaching notification = %q, %q, %q, %q", preference, typ, title, body)
	}
	preference, typ, title, body = slaNotification("breached")
	if preference != "sla_breach" || typ != "sla_breach" || title == "" || body == "" {
		t.Fatalf("breached notification = %q, %q, %q, %q", preference, typ, title, body)
	}
	if preference, typ, title, body = slaNotification("met"); preference != "" || typ != "" || title != "" || body != "" {
		t.Fatalf("unknown SLA state returned notification = %q, %q, %q, %q", preference, typ, title, body)
	}
}

func TestPreferenceTypeForNotification(t *testing.T) {
	if got := preferenceTypeFor("customer_reply"); got != "reply" {
		t.Fatalf("customer reply preference type = %q, want reply", got)
	}
	if got := preferenceTypeFor("assignment"); got != "assignment" {
		t.Fatalf("assignment preference type = %q, want assignment", got)
	}
}

func TestTicketCustomerMessage(t *testing.T) {
	subject, body := ticketCustomerMessage(events.TicketCreated, " Ada ", "HC-42", "Cannot sign in", "in_progress")
	if subject != "Ticket HC-42 received" || body != "Hi Ada,\n\nWe received your request “Cannot sign in”. Its current status is in progress." {
		t.Fatalf("creation message = %q / %q", subject, body)
	}
	subject, body = ticketCustomerMessage(events.TicketStateSet, "Ada", "HC-42", "Cannot sign in", "resolved")
	if subject != "Ticket HC-42 update" || body != "Hi Ada,\n\nYour ticket “Cannot sign in” is now resolved." {
		t.Fatalf("status message = %q / %q", subject, body)
	}
}

func TestProcessEventDispatchesSurveysOnlyForTerminalTicketStates(t *testing.T) {
	dispatcher := &fakeSurveyDispatcher{}
	service := New(nil)
	service.SetSurveyDispatcher(dispatcher)
	for _, state := range []string{"open", "pending", "resolved"} {
		dispatcher.status = ""
		record := events.Record{ID: "evt-" + state, WorkspaceID: "ws-1", EntityType: "ticket", EntityID: "tic-1", Type: events.TicketStateSet, Data: []byte(`{"to":"` + state + `"}`)}
		if err := service.processEvent(context.Background(), record); err != nil {
			t.Fatalf("process %s: %v", state, err)
		}
		if state == "resolved" && dispatcher.status != state {
			t.Fatalf("resolved state dispatched as %q", dispatcher.status)
		}
		if state != "resolved" && dispatcher.status != "" {
			t.Fatalf("non-terminal state %s dispatched as %q", state, dispatcher.status)
		}
	}
}

func TestChangelogLinkUsesConfiguredPublicURL(t *testing.T) {
	base, err := url.Parse("https://support.example.test/base")
	if err != nil {
		t.Fatal(err)
	}
	service := New(nil)
	service.SetPublicURL(base)
	if got := service.changelogLink("chg_123"); got != "https://support.example.test/base/portal/changelog#chg_123" {
		t.Fatalf("changelog link = %q", got)
	}
}
