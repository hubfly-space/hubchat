package api

import (
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
)

func TestSavedViewCapabilitiesFollowEntityType(t *testing.T) {
	tests := []struct {
		name       string
		actor      *authorization.Actor
		entityType string
		read       bool
		write      bool
	}{
		{
			name:       "ticket manager can use ticket views only",
			actor:      &authorization.Actor{Role: "agent", Capabilities: map[authorization.Capability]bool{authorization.TicketManage: true}},
			entityType: "ticket",
			read:       true,
			write:      true,
		},
		{
			name:       "conversation assigner can write conversation views",
			actor:      &authorization.Actor{Role: "agent", Capabilities: map[authorization.Capability]bool{authorization.ConversationRead: true, authorization.ConversationAssign: true}},
			entityType: "conversation",
			read:       true,
			write:      true,
		},
		{
			name:       "ticket manager cannot write conversation views",
			actor:      &authorization.Actor{Role: "agent", Capabilities: map[authorization.Capability]bool{authorization.TicketManage: true}},
			entityType: "conversation",
			read:       false,
			write:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canReadSavedView(test.actor, test.entityType); got != test.read {
				t.Fatalf("read permission = %t, want %t", got, test.read)
			}
			if got := canWriteSavedView(test.actor, test.entityType); got != test.write {
				t.Fatalf("write permission = %t, want %t", got, test.write)
			}
		})
	}
}
