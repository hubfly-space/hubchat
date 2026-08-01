package automation

import (
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/events"
)

func TestValidateActionsAssignsStableIDs(t *testing.T) {
	actions := []Action{{Type: "add_tag"}, {Type: "set_priority"}}
	if err := validateActions(actions); err != nil {
		t.Fatalf("validate actions: %v", err)
	}
	if actions[0].ID != "action_1" || actions[1].ID != "action_2" {
		t.Fatalf("unexpected action ids: %+v", actions)
	}
}

func TestValidateActionsRejectsUnknownAction(t *testing.T) {
	if err := validateActions([]Action{{Type: "invent_response"}}); err != ErrInvalidAction {
		t.Fatalf("expected invalid action, got %v", err)
	}
}

func TestMatchesOnlyEmptyConditionsUntilSubjectAdapterExists(t *testing.T) {
	if !matches(nil) || matches(map[string]any{"all": []any{map[string]any{"field": "priority"}}}) {
		t.Fatal("condition matching must be conservative")
	}
}

func TestMatchesDataEvaluatesDeterministicOperators(t *testing.T) {
	conditions := map[string]any{
		"match": "all",
		"conditions": []any{
			map[string]any{"field": "priority", "operator": "is", "value": "urgent"},
			map[string]any{"field": "channel", "operator": "in", "value": []any{"email", "widget"}},
		},
	}
	if !matchesData(conditions, map[string]any{"priority": "urgent", "channel": "email"}) {
		t.Fatal("expected conditions to match")
	}
	if matchesData(conditions, map[string]any{"priority": "normal", "channel": "email"}) {
		t.Fatal("unexpected condition match")
	}
}

func TestTriggerForPublicationEvents(t *testing.T) {
	for eventType, want := range map[events.Type]string{
		events.ArticlePublished:   "article.published",
		events.ChangelogPublished: "changelog.published",
	} {
		if got := triggerForEvent(eventType); got != want {
			t.Fatalf("triggerForEvent(%q) = %q, want %q", eventType, got, want)
		}
	}
}

func TestValidateContentScopes(t *testing.T) {
	if err := validateContent("reply", "personal", "", "", nil); err != ErrInvalidTarget {
		t.Fatalf("expected personal owner validation, got %v", err)
	}
	if err := validateContent("reply", "team", "", "", nil); err != ErrInvalidTarget {
		t.Fatalf("expected team target validation, got %v", err)
	}
	if err := validateContent("reply", "unknown", "", "", nil); err != ErrInvalidScope {
		t.Fatalf("expected scope validation, got %v", err)
	}
}

func TestMacroCapabilitiesAreCheckedPerActionAndSubject(t *testing.T) {
	conversationActor := &authorization.Actor{
		Role: "agent",
		Capabilities: map[authorization.Capability]bool{
			authorization.AutomationManage:  true,
			authorization.ConversationReply: true,
		},
	}
	if err := validateMacroCapabilities(conversationActor, "conversation", []Action{{Type: "send_message"}}); err != nil {
		t.Fatalf("reply-capable actor rejected: %v", err)
	}
	if err := validateMacroCapabilities(conversationActor, "conversation", []Action{{Type: "set_priority"}}); err == nil {
		t.Fatal("expected conversation.assign to be required for set_priority")
	}
	if _, ok := macroActionCapability("send_message", "ticket"); ok {
		t.Fatal("send_message must not be executable against a ticket")
	}
}

func TestMacroCapabilitiesDeduplicateChecks(t *testing.T) {
	actor := &authorization.Actor{Role: "agent", Capabilities: map[authorization.Capability]bool{
		authorization.AutomationManage: true,
		authorization.TicketManage:     true,
	}}
	actions := []Action{{Type: "set_priority"}, {Type: "set_state"}, {Type: "add_tag"}}
	if err := validateMacroCapabilities(actor, "ticket", actions); err != nil {
		t.Fatalf("ticket action bundle rejected: %v", err)
	}
}
