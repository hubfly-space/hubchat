// Package searchmodel contains the wire-neutral search contract shared by
// owning domains and the search coordinator, without database dependencies.
package searchmodel

import "time"

type Match struct{ Kind, Title, Snippet, EntityID, ConversationID string }
type Filters struct {
	Kinds          []string
	Channel, TagID string
	After, Before  *time.Time
}
