package authorization

import (
	"context"
	"fmt"
)

// Nil means workspace-wide access. A non-nil empty slice means no inboxes.
func (a *Actor) CanSeeInbox(inboxID string) bool {
	if a == nil || a.AllowedInboxIDs == nil {
		return true
	}
	for _, id := range a.AllowedInboxIDs {
		if id == inboxID {
			return true
		}
	}
	return false
}
func CanSeeInbox(ctx context.Context, inboxID string) bool {
	return FromContext(ctx).CanSeeInbox(inboxID)
}
func InboxIDs(ctx context.Context) []string {
	if a := FromContext(ctx); a != nil {
		return a.AllowedInboxIDs
	}
	return nil
}
func InboxPredicate(ctx context.Context, column string, parameter int) (string, []any) {
	if ids := InboxIDs(ctx); ids != nil {
		return fmt.Sprintf("%s = ANY($%d::text[])", column, parameter), []any{ids}
	}
	return "TRUE", nil
}
