package customer

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

// ErrInvalidBlockKind is returned when kind is not one blocked_contacts accepts.
var ErrInvalidBlockKind = errors.New("customer: not a recognised block kind")

// Service covers a customer's basic profile, tags, company links and
// roster, blocking, the metadata allowlist, event ingestion, contact
// sessions, and identity merge (§6.9, §6.10, §26.3, §26.4).
type Service struct {
	repo   *repository
	pool   *database.Pool
	audit  *audit.Log
	events *events.Log

	// maxEventBytes and maxAttributesPerRecord enforce cfg.Limits at the
	// service layer, so every entry point (dashboard API today, the widget
	// SDK once Stage 5 exists) is bounded the same way rather than each
	// handler re-implementing the check.
	maxEventBytes          int64
	maxAttributesPerRecord int
}

func New(pool *database.Pool, eventLog *events.Log, auditLog *audit.Log, limits config.Limits) *Service {
	return &Service{
		repo: &repository{pool: pool}, pool: pool, audit: auditLog, events: eventLog,
		maxEventBytes: limits.MaxEventBytes, maxAttributesPerRecord: limits.MaxAttributesPerCustomer,
	}
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

// FindByExternalID resolves an import/SDK-owned identifier without widening
// the workspace boundary. Callers use ErrNotFound to distinguish a new row
// from a conflict that should be updated.
func (s *Service) FindByExternalID(ctx context.Context, workspaceID, externalID string) (*Customer, error) {
	return s.repo.byExternalID(ctx, workspaceID, strings.TrimSpace(externalID))
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

// Identify resolves a widget/SDK identify() call to a customer record,
// creating one if this is the first time this person has been seen.
//
// verified marks whether the caller already proved this identity (a signed
// HS256 token, checked by the widget layer before this is ever called) as
// opposed to a bare claim typed into identify({email}) — see byVerifiedEmail
// for why that distinction gates matching by email at all (§6.9: never merge
// on weak signals). externalID has no such caveat: it is the workspace's own
// system assigning its own id, so matching on it is never a spoofable claim
// about *someone else's* identity in the way an unverified email is.
//
// existingCustomerID narrows the search to a specific record already linked
// to this visitor (e.g. from a prior anonymous message) — when set and the
// lookup below finds a *different* verified match, the two are deliberately
// left unmerged and the existing record is returned unchanged; reconciling
// two independently-established identities is a merge, and merges happen
// through Merge/MergePreview with an agent looking at both sides, not as a
// side effect of a page load.
func (s *Service) Identify(
	ctx context.Context, workspaceID string,
	existingCustomerID *string,
	name, email, externalID *string,
	verified bool,
) (*Customer, error) {
	if existingCustomerID != nil {
		if existing, err := s.repo.byID(ctx, workspaceID, *existingCustomerID); err == nil {
			return s.touchIdentity(ctx, workspaceID, existing, name, email, externalID, verified)
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	if externalID != nil && *externalID != "" {
		if found, err := s.repo.byExternalID(ctx, workspaceID, *externalID); err == nil {
			return s.touchIdentity(ctx, workspaceID, found, name, email, externalID, verified)
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	if verified && email != nil && *email != "" {
		if found, err := s.repo.byVerifiedEmail(ctx, workspaceID, *email); err == nil {
			return s.touchIdentity(ctx, workspaceID, found, name, email, externalID, verified)
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}

	verification := "unverified"
	if verified {
		verification = "verified"
	}

	id := ids.New(ids.PrefixCustomer)
	var created *Customer
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		created, err = s.repo.insertCustomer(ctx, id, workspaceID, name, email, externalID, verification)
		if err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.CustomerCreated,
			EntityType: "customer", EntityID: id, ActorType: events.ActorCustomer, ActorID: id,
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// touchIdentity records a repeat contact from an already-known customer,
// inside a transaction so the customer.identified event commits with it.
func (s *Service) touchIdentity(
	ctx context.Context, workspaceID string, existing *Customer,
	name, email, externalID *string, verified bool,
) (*Customer, error) {
	var updated *Customer
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		updated, err = s.repo.touchIdentity(ctx, workspaceID, existing.ID, name, email, externalID, verified)
		if err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.CustomerIdentified,
			EntityType: "customer", EntityID: existing.ID, ActorType: events.ActorCustomer, ActorID: existing.ID,
		})
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// SetOwner assigns (or clears, when ownerID is nil) which agent owns this
// customer's account — the "Assign owner" bulk action in the directory.
func (s *Service) SetOwner(ctx context.Context, workspaceID, actorMemberID, customerID string, ownerID *string) (*Customer, error) {
	if ownerID != nil {
		ok, err := s.repo.memberInWorkspace(ctx, workspaceID, *ownerID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrInvalidOwner
		}
	}
	if err := s.repo.setOwner(ctx, workspaceID, customerID, ownerID); err != nil {
		return nil, err
	}
	return s.repo.byID(ctx, workspaceID, customerID)
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
