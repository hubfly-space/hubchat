package conversation

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

// SetAssignee assigns a conversation to a member, or clears the assignee when
// assigneeID is nil. Assigning is not itself a state transition — a
// conversation can be assigned while still "new" — but assigning an
// unassigned conversation typically accompanies moving it to "open"; the
// caller decides that combination explicitly rather than this method
// guessing at it.
func (s *Service) SetAssignee(
	ctx context.Context, workspaceID, actorMemberID, conversationID string, assigneeID *string,
) (*Conversation, error) {
	if assigneeID != nil {
		ok, err := s.repo.memberInWorkspace(ctx, workspaceID, *assigneeID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrInvalidAssignee
		}
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		conv, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return err
		}
		if err := s.repo.setAssignee(ctx, tx, conversationID, assigneeID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "conversation.assigned", EntityType: entityConversation, EntityID: conversationID,
			Metadata: map[string]any{"assignee_id": derefOr(assigneeID, ""), "previous_assignee_id": derefOr(conv.AssigneeID, "")},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationAssigned,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "team_id": conv.TeamID, "assignee_id": assigneeID},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, conversationID)
}

// SetTeam assigns a conversation to a team, or clears it when teamID is nil.
func (s *Service) SetTeam(
	ctx context.Context, workspaceID, actorMemberID, conversationID string, teamID *string,
) (*Conversation, error) {
	if teamID != nil {
		ok, err := s.repo.teamInWorkspace(ctx, workspaceID, *teamID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrInvalidTeam
		}
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		conv, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return err
		}
		if err := s.repo.setTeam(ctx, tx, conversationID, teamID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "conversation.updated", EntityType: entityConversation, EntityID: conversationID,
			Metadata: map[string]any{"team_id": derefOr(teamID, "")},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationAssigned,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "team_id": teamID, "assignee_id": conv.AssigneeID},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, conversationID)
}

// SetInbox moves a conversation to a different inbox — "move to inbox" in
// the conversation panel's overflow menu.
func (s *Service) SetInbox(
	ctx context.Context, workspaceID, actorMemberID, conversationID, inboxID string,
) (*Conversation, error) {
	ok, err := s.repo.inboxInWorkspace(ctx, workspaceID, inboxID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidInbox
	}

	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		conv, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return err
		}
		if err := s.repo.setInbox(ctx, tx, conversationID, inboxID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "conversation.updated", EntityType: entityConversation, EntityID: conversationID,
			Metadata: map[string]any{"inbox_id": inboxID, "previous_inbox_id": conv.InboxID},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationUpdated,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "inbox_id": inboxID},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, conversationID)
}

// SetPriority changes urgency. Purely a label — it drives no automatic
// behaviour on its own, though automation rules (once built) can react to it.
func (s *Service) SetPriority(
	ctx context.Context, workspaceID, actorMemberID, conversationID, priority string,
) (*Conversation, error) {
	if !validPriorities[priority] {
		return nil, ErrInvalidPriority
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		if err := s.repo.setPriority(ctx, tx, conversationID, priority); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "conversation.updated", EntityType: entityConversation, EntityID: conversationID,
			Metadata: map[string]any{"priority": priority},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationUpdated,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "priority": priority},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, conversationID)
}

// SetState moves a conversation between the non-snoozed states, recording the
// transition in the append-only status history that time-in-state reporting
// reads from. Moving to or from "snoozed" always goes through Snooze/this
// method's automatic clearing, never a bare state string, because a snooze
// without a wake time is a conversation nobody will ever revisit.
func (s *Service) SetState(
	ctx context.Context, workspaceID, actorMemberID, conversationID, state string,
) (*Conversation, error) {
	if !validStates[state] {
		return nil, ErrInvalidState
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		conv, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return err
		}
		if conv.State == state {
			return nil
		}

		if err := s.repo.setState(ctx, tx, conversationID, state); err != nil {
			return err
		}
		if err := s.repo.insertStatusHistory(
			ctx, tx, ids.New(ids.PrefixStatusHistory), conversationID, conv.State, state, "member", actorMemberID,
		); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "conversation.state_changed", EntityType: entityConversation, EntityID: conversationID,
			Metadata: map[string]any{"from": conv.State, "to": state},
		}); err != nil {
			return err
		}

		eventType := events.ConversationStateSet
		if state == "resolved" {
			eventType = events.ConversationResolved
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: eventType,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "from": conv.State, "to": state},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, conversationID)
}

// Snooze moves a conversation to the snoozed state until a specific time. A
// scheduled job (registered alongside the durable job queue) wakes it back to
// "open" when that time passes — see cmd/hubchat's job handler registration.
func (s *Service) Snooze(
	ctx context.Context, workspaceID, actorMemberID, conversationID string, until time.Time,
) (*Conversation, error) {
	if !until.After(time.Now()) {
		return nil, ErrSnoozeInPast
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		conv, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return err
		}
		if err := s.repo.snooze(ctx, tx, conversationID, until); err != nil {
			return err
		}
		if err := s.repo.insertStatusHistory(
			ctx, tx, ids.New(ids.PrefixStatusHistory), conversationID, conv.State, "snoozed", "member", actorMemberID,
		); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "conversation.state_changed", EntityType: entityConversation, EntityID: conversationID,
			Metadata: map[string]any{"from": conv.State, "to": "snoozed", "until": until.UTC().Format(time.RFC3339)},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationStateSet,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "from": conv.State, "to": "snoozed"},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, conversationID)
}

// JobWakeSnoozed is the recurring job type that calls WakeSnoozed. It
// re-enqueues itself after every run (cmd/hubchat's handler), so the queue
// worker's own backoff and dead-lettering machinery is what keeps it alive
// rather than a separate scheduler process.
const JobWakeSnoozed = "conversation.wake_snoozed"

// WakeSnoozed reopens every conversation whose snooze has elapsed. Called by
// the scheduler on a timer (cmd/hubchat), not from any HTTP path — a snooze
// wakes on its own, nobody polls for it.
func (s *Service) WakeSnoozed(ctx context.Context) (int, error) {
	woken, err := s.repo.wakeSnoozed(ctx)
	if err != nil {
		return 0, err
	}
	for _, w := range woken {
		err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
			if err := s.repo.insertStatusHistory(
				ctx, tx, ids.New(ids.PrefixStatusHistory), w.ID, "snoozed", "open", "system", "",
			); err != nil {
				return err
			}
			return s.appendEvent(ctx, tx, events.Event{
				WorkspaceID: w.WorkspaceID, Type: events.ConversationStateSet,
				EntityType: entityConversation, EntityID: w.ID, ActorType: events.ActorSystem,
				Data: map[string]any{"conversation_id": w.ID, "from": "snoozed", "to": "open"},
			})
		})
		if err != nil {
			return 0, err
		}
	}
	return len(woken), nil
}

// AddTag and RemoveTag manage the conversation's workspace-wide tag set.
// Idempotent in both directions, matching the same reasoning
// AddTeamMember/RemoveTeamMember document: the caller's desired end state
// already holds either way.
func (s *Service) AddTag(ctx context.Context, workspaceID, actorMemberID, conversationID, tagID string) error {
	ok, err := s.repo.tagInWorkspace(ctx, workspaceID, tagID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTagNotFound
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		if err := s.repo.addTag(ctx, tx, conversationID, tagID); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationUpdated,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "tag_added": tagID},
		})
	})
}

func (s *Service) RemoveTag(ctx context.Context, workspaceID, actorMemberID, conversationID, tagID string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		if err := s.repo.removeTag(ctx, tx, conversationID, tagID); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationUpdated,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "tag_removed": tagID},
		})
	})
}

// Tags returns the tag ids on a conversation, for the DTO.
func (s *Service) Tags(ctx context.Context, workspaceID, conversationID string) ([]string, error) {
	return s.repo.tagIDs(ctx, workspaceID, conversationID)
}

// TagsForMany batches Tags for a whole list page in one query.
func (s *Service) TagsForMany(ctx context.Context, workspaceID string, conversationIDs []string) (map[string][]string, error) {
	return s.repo.tagIDsForMany(ctx, workspaceID, conversationIDs)
}

// Follow and Unfollow manage who receives updates about a conversation
// beyond its assignee.
func (s *Service) Follow(ctx context.Context, workspaceID, conversationID, memberID string) error {
	ok, err := s.repo.memberInWorkspace(ctx, workspaceID, memberID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidAssignee
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		return s.repo.follow(ctx, tx, conversationID, memberID)
	})
}

func (s *Service) Unfollow(ctx context.Context, workspaceID, conversationID, memberID string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		return s.repo.unfollow(ctx, tx, conversationID, memberID)
	})
}

func (s *Service) Followers(ctx context.Context, workspaceID, conversationID string) ([]string, error) {
	return s.repo.followerIDs(ctx, workspaceID, conversationID)
}

// FollowersPage returns follower member IDs in stable order for the API's
// opaque cursor pagination contract.
func (s *Service) FollowersPage(ctx context.Context, workspaceID, conversationID, before string, limit int) ([]string, error) {
	return s.repo.followerIDsPage(ctx, workspaceID, conversationID, before, limit)
}

// MarkRead records that memberID has seen everything in the conversation up
// to its latest message. Read state tracks the newest message only — a
// per-message read receipt for every reader would be a write on every open
// for no query this product makes.
func (s *Service) MarkRead(ctx context.Context, workspaceID, conversationID, memberID string) error {
	latest, err := s.repo.latestMessageID(ctx, workspaceID, conversationID)
	if err != nil {
		return err
	}
	if latest == "" {
		return nil
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.repo.markRead(ctx, tx, latest, "agent", memberID)
	})
}

// IsRead reports whether memberID has read the latest message — the negation
// the Conversation DTO's "unread" field publishes.
func (s *Service) IsRead(ctx context.Context, workspaceID, conversationID, memberID string) (bool, error) {
	return s.repo.isRead(ctx, workspaceID, conversationID, "agent", memberID)
}

// IsReadForMany batches IsRead for a whole list page in two queries total.
func (s *Service) IsReadForMany(
	ctx context.Context, workspaceID string, conversationIDs []string, memberID string,
) (map[string]bool, error) {
	return s.repo.isReadForMany(ctx, workspaceID, conversationIDs, "agent", memberID)
}
