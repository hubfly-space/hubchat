package notification

import (
	"errors"
	"testing"

	"github.com/hubchat/hubchat/internal/events"
)

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
