package realtime

import "github.com/hubchat/hubchat/internal/conversation"

// ConversationBroadcaster adapts Hub to conversation.Broadcaster.
//
// This tiny file is the entire coupling between the two modules (§14
// boundary rules: cross-module work is an explicit call, not a shared
// table). conversation.Service holds only the interface; it never imports
// realtime, so the dependency points one way.
type ConversationBroadcaster struct {
	Hub *Hub
}

func (b ConversationBroadcaster) BroadcastMessage(workspaceID, conversationID string, message conversation.Message) {
	b.Hub.Publish(workspaceID, "message.created", message.Sequence, map[string]any{
		"conversation_id": conversationID,
		"message": map[string]any{
			"id":          message.ID,
			"client_id":   message.ClientID,
			"kind":        message.Kind,
			"author_type": message.AuthorType,
			"author_id":   message.AuthorID,
			"author_name": message.AuthorName,
			"body":        message.Body,
			"sequence":    message.Sequence,
			"created_at":  message.CreatedAt,
		},
	})
}
