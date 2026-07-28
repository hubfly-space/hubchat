package ticket

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

// SetAssignee assigns a ticket to a member, or clears the assignee when
// assigneeID is nil.
func (s *Service) SetAssignee(ctx context.Context, workspaceID, actorMemberID, id string, assigneeID *string) (*Ticket, error) {
	if assigneeID != nil {
		if ok, err := s.repo.memberInWorkspace(ctx, workspaceID, *assigneeID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidAssignee
		}
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.setAssignee(ctx, tx, id, assigneeID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "ticket.assigned", EntityType: entityTicket, EntityID: id,
			Metadata: map[string]any{"assignee_id": derefOr(assigneeID, "")},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "assignee_id": assigneeID},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) SetTeam(ctx context.Context, workspaceID, actorMemberID, id string, teamID *string) (*Ticket, error) {
	if teamID != nil {
		if ok, err := s.repo.teamInWorkspace(ctx, workspaceID, *teamID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidTeam
		}
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.setTeam(ctx, tx, id, teamID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "ticket.updated", EntityType: entityTicket, EntityID: id,
			Metadata: map[string]any{"team_id": derefOr(teamID, "")},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "team_id": teamID},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) SetInbox(ctx context.Context, workspaceID, actorMemberID, id, inboxID string) (*Ticket, error) {
	if ok, err := s.repo.inboxInWorkspace(ctx, workspaceID, inboxID); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrInvalidInbox
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.setInbox(ctx, tx, id, &inboxID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "ticket.updated", EntityType: entityTicket, EntityID: id,
			Metadata: map[string]any{"inbox_id": inboxID},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "inbox_id": inboxID},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) SetPriority(ctx context.Context, workspaceID, actorMemberID, id, priority string) (*Ticket, error) {
	if !validPriorities[priority] {
		return nil, ErrInvalidPriority
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.setPriority(ctx, tx, id, priority); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "ticket.updated", EntityType: entityTicket, EntityID: id,
			Metadata: map[string]any{"priority": priority},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "priority": priority},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

// SetCustomer changes which customer a ticket belongs to, deriving the
// customer's primary company the same way Create does — the caller sets a
// person, not a company, because the company follows from who the person is.
func (s *Service) SetCustomer(ctx context.Context, workspaceID, actorMemberID, id string, customerID *string) (*Ticket, error) {
	var companyID *string
	if customerID != nil {
		if ok, err := s.repo.customerInWorkspace(ctx, workspaceID, *customerID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidCustomer
		}
		derived, err := s.repo.primaryCompanyID(ctx, *customerID)
		if err != nil {
			return nil, err
		}
		companyID = derived
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.setCustomer(ctx, tx, id, customerID, companyID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "ticket.updated", EntityType: entityTicket, EntityID: id,
			Metadata: map[string]any{"customer_id": derefOr(customerID, "")},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "customer_id": customerID},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) SetDueAt(ctx context.Context, workspaceID, actorMemberID, id string, dueAt *time.Time) (*Ticket, error) {
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.setDueAt(ctx, tx, id, dueAt); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "due_at": dueAt},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

// SetStatus moves a ticket between the workflow's statuses, applying the
// resolved/closed/reopen bookkeeping the migration's own comment specifies:
// first_resolved_at is set once and never cleared, resolved_at/closed_at
// track the *current* resolution, and reopen_count increments only when a
// previously resolved-or-closed ticket comes back to life (§6.3 reopen
// rules).
func (s *Service) SetStatus(ctx context.Context, workspaceID, actorMemberID, id, status string) (*Ticket, error) {
	if !validStatuses[status] {
		return nil, ErrInvalidStatus
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		t, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}
		if t.Status == status {
			return nil
		}

		wasDone := t.Status == "resolved" || t.Status == "closed"
		isDone := status == "resolved" || status == "closed"

		firstResolvedAt := t.FirstResolvedAt
		resolvedAt := t.ResolvedAt
		closedAt := t.ClosedAt
		reopenCount := t.ReopenCount

		now := time.Now().UTC()
		switch {
		case isDone && !wasDone:
			if firstResolvedAt == nil {
				firstResolvedAt = &now
			}
			resolvedAt = &now
			if status == "closed" {
				closedAt = &now
			}
		case wasDone && !isDone:
			reopenCount++
			resolvedAt = nil
			closedAt = nil
		case status == "closed" && t.Status == "resolved":
			closedAt = &now
		}

		if err := s.repo.setStatus(ctx, tx, id, status, firstResolvedAt, resolvedAt, closedAt, reopenCount); err != nil {
			return err
		}
		if err := s.repo.insertStatusHistory(ctx, tx, ids.New(ids.PrefixStatusHistory), id, t.Status, status, "member", actorMemberID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "ticket.state_changed", EntityType: entityTicket, EntityID: id,
			Metadata: map[string]any{"from": t.Status, "to": status},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketStateSet,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "from": t.Status, "to": status},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

// AddTag and RemoveTag manage the ticket's workspace-wide tag set. Idempotent
// in both directions: the caller's desired end state already holds either
// way.
func (s *Service) AddTag(ctx context.Context, workspaceID, actorMemberID, id, tagID string) error {
	if ok, err := s.repo.tagInWorkspace(ctx, workspaceID, tagID); err != nil {
		return err
	} else if !ok {
		return ErrTagNotFound
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.addTag(ctx, tx, id, tagID); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "tag_added": tagID},
		})
	})
}

func (s *Service) RemoveTag(ctx context.Context, workspaceID, actorMemberID, id, tagID string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.removeTag(ctx, tx, id, tagID); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "tag_removed": tagID},
		})
	})
}

func (s *Service) Tags(ctx context.Context, workspaceID, id string) ([]string, error) {
	return s.repo.tagIDs(ctx, workspaceID, id)
}

func (s *Service) TagsForMany(ctx context.Context, workspaceID string, ids []string) (map[string][]string, error) {
	return s.repo.tagIDsForMany(ctx, workspaceID, ids)
}

// Follow and Unfollow manage watchers beyond the assignee (§6.3 watchers and
// followers).
func (s *Service) Follow(ctx context.Context, workspaceID, id, memberID string) error {
	if ok, err := s.repo.memberInWorkspace(ctx, workspaceID, memberID); err != nil {
		return err
	} else if !ok {
		return ErrInvalidAssignee
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		return s.repo.follow(ctx, tx, id, memberID)
	})
}

func (s *Service) Unfollow(ctx context.Context, workspaceID, id, memberID string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		return s.repo.unfollow(ctx, tx, id, memberID)
	})
}

func (s *Service) Followers(ctx context.Context, workspaceID, id string) ([]string, error) {
	return s.repo.followerIDs(ctx, workspaceID, id)
}

// Link records a directed relationship between two tickets — related,
// duplicate_of, blocks, or blocked_by (§6.3 linked tickets).
func (s *Service) Link(ctx context.Context, workspaceID, actorMemberID, sourceID, targetID, relation string) error {
	if !validLinkRelations[relation] {
		return ErrInvalidRelation
	}
	if sourceID == targetID {
		return ErrLinkToSelf
	}
	if ok, err := s.repo.exists(ctx, workspaceID, targetID); err != nil {
		return err
	} else if !ok {
		return ErrInvalidParent
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, sourceID); err != nil {
			return err
		}
		if err := s.repo.addLink(ctx, tx, ids.New(ids.PrefixTicketLink), workspaceID, sourceID, targetID, relation); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "ticket.linked", EntityType: entityTicket, EntityID: sourceID,
			Metadata: map[string]any{"target_id": targetID, "relation": relation},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: sourceID, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": sourceID, "linked_to": targetID, "relation": relation},
		})
	})
}

func (s *Service) Unlink(ctx context.Context, workspaceID, sourceID, targetID, relation string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.repo.removeLink(ctx, tx, workspaceID, sourceID, targetID, relation)
	})
}

func (s *Service) Links(ctx context.Context, workspaceID, id string) ([]TicketLink, error) {
	return s.repo.links(ctx, workspaceID, id)
}

// SetParent makes id a child of parentID, or clears the relationship when
// parentID is nil. Rejects the one-hop self-parent (already caught by the
// tickets_parent_not_self CHECK, but returning a typed error here means the
// caller never sees a raw constraint violation) and any cycle a deeper chain
// would create — id cannot become the parent of its own ancestor.
func (s *Service) SetParent(ctx context.Context, workspaceID, actorMemberID, id string, parentID *string) (*Ticket, error) {
	if parentID != nil {
		if *parentID == id {
			return nil, ErrParentIsSelf
		}
		if ok, err := s.repo.exists(ctx, workspaceID, *parentID); err != nil {
			return nil, err
		} else if !ok {
			return nil, ErrInvalidParent
		}
		ancestors, err := s.repo.ancestorIDs(ctx, workspaceID, *parentID)
		if err != nil {
			return nil, err
		}
		for _, ancestorID := range ancestors {
			if ancestorID == id {
				return nil, ErrParentCycle
			}
		}
	}

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockAndLoad(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		if err := s.repo.setParent(ctx, tx, id, parentID); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.TicketUpdated,
			EntityType: entityTicket, EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": id, "parent_id": parentID},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) Children(ctx context.Context, workspaceID, id string) ([]string, error) {
	return s.repo.childIDs(ctx, workspaceID, id)
}

// DuplicateCandidates surfaces other open tickets that plausibly describe
// the same issue as a not-yet-created (or being-edited) ticket, so the
// composer can warn before a duplicate is filed (§6.3 duplicate detection).
func (s *Service) DuplicateCandidates(ctx context.Context, workspaceID, excludeID, title string, customerID, companyID *string) ([]Ticket, error) {
	return s.repo.duplicateCandidates(ctx, workspaceID, excludeID, title, customerID, companyID)
}

func derefOr(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
