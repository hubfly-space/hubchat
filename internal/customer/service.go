package customer

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

// ErrInvalidBlockKind is returned when kind is not one blocked_contacts accepts.
var ErrInvalidBlockKind = errors.New("customer: not a recognised block kind")

// Service is deliberately narrow: a customer's basic profile, tags, company
// links, and blocking — enough for the inbox's customer panel and the
// "block this visitor" action a conversation exposes. Identity merging,
// attribute definitions, contact sessions, and event ingestion are Stage 4
// (§26.3's sharpest edge, not something to bolt on as a side effect of
// wiring the inbox).
type Service struct {
	repo   *repository
	pool   *database.Pool
	audit  *audit.Log
	events *events.Log
}

func New(pool *database.Pool, eventLog *events.Log, auditLog *audit.Log) *Service {
	return &Service{repo: &repository{pool: pool}, pool: pool, audit: auditLog, events: eventLog}
}

func (s *Service) appendEvent(ctx context.Context, tx pgx.Tx, event events.Event) error {
	if s.events == nil {
		return nil
	}
	_, err := s.events.Append(ctx, tx, event)
	return err
}

func (s *Service) recordAudit(ctx context.Context, tx pgx.Tx, entry audit.Entry) error {
	if s.audit == nil {
		return nil
	}
	if entry.ActorName == "" && entry.ActorType == audit.ActorUser && entry.ActorID != "" {
		if name, err := s.repo.memberDisplayName(ctx, entry.ActorID); err == nil {
			entry.ActorName = name
		}
	}
	return audit.RecordTx(ctx, tx, entry)
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Customer, error) {
	return s.repo.byID(ctx, workspaceID, id)
}

// GetMany batch-loads customers by id, for rendering a page of conversations
// that each reference one.
func (s *Service) GetMany(ctx context.Context, workspaceID string, ids []string) ([]Customer, error) {
	return s.repo.byIDs(ctx, workspaceID, ids)
}

// Search matches on name and email — the customer picker's directory search
// and the inbox's "find this person" lookup.
func (s *Service) Search(ctx context.Context, workspaceID, query string, limit int) ([]Customer, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.search(ctx, workspaceID, strings.TrimSpace(query), limit)
}

// Update changes the editable fields of a customer's profile, under
// optimistic concurrency: expectedVersion must match what is currently
// stored, or ErrVersionConflict tells the caller their view was stale rather
// than silently clobbering a concurrent edit.
func (s *Service) Update(
	ctx context.Context, workspaceID, actorMemberID, id string, expectedVersion int,
	name, email, phone, language, timezone *string,
) (*Customer, error) {
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.update(ctx, workspaceID, id, expectedVersion, name, email, phone, language, timezone); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "customer.updated", EntityType: "customer", EntityID: id,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "customer.updated",
			EntityType: "customer", EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) AddTag(ctx context.Context, workspaceID, actorMemberID, customerID, tagID string) error {
	ok, err := s.repo.tagInWorkspace(ctx, workspaceID, tagID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTagNotFound
	}
	if _, err := s.repo.byID(ctx, workspaceID, customerID); err != nil {
		return err
	}
	return s.repo.addTag(ctx, customerID, tagID)
}

func (s *Service) RemoveTag(ctx context.Context, workspaceID, actorMemberID, customerID, tagID string) error {
	if _, err := s.repo.byID(ctx, workspaceID, customerID); err != nil {
		return err
	}
	return s.repo.removeTag(ctx, customerID, tagID)
}

func (s *Service) Tags(ctx context.Context, workspaceID, customerID string) ([]string, error) {
	return s.repo.tagIDs(ctx, workspaceID, customerID)
}

func (s *Service) CompanyIDs(ctx context.Context, workspaceID, customerID string) ([]string, error) {
	return s.repo.companyIDs(ctx, workspaceID, customerID)
}

// validBlockKinds mirrors blocked_contacts' CHECK constraint (migration 0006).
var validBlockKinds = map[string]bool{
	"email": true, "domain": true, "ip": true, "visitor": true, "customer": true,
}

// Block records that a contact (a customer, a visitor, or the email/domain/IP
// behind one) should be refused — the "block this visitor" action a
// conversation panel exposes, separate from marking the conversation itself
// as spam.
func (s *Service) Block(ctx context.Context, workspaceID, actorMemberID, kind, value string, reason *string) error {
	if !validBlockKinds[kind] {
		return ErrInvalidBlockKind
	}
	value = strings.TrimSpace(value)

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.blockContact(ctx, ids.New(ids.PrefixBlockedContact), workspaceID, kind, value, reason, actorMemberID); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "contact.blocked", EntityType: "blocked_contact", EntityID: value,
			Metadata: map[string]any{"kind": kind},
		})
	})
}
