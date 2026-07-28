package inbox

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
		if name, err := s.repo.memberDisplayName(ctx, tx, entry.ActorID); err == nil {
			entry.ActorName = name
		}
	}
	return audit.RecordTx(ctx, tx, entry)
}

func normalizeChannels(channels []string) ([]string, error) {
	if len(channels) == 0 {
		return []string{"manual"}, nil
	}
	out := make([]string, 0, len(channels))
	seen := map[string]bool{}
	for _, c := range channels {
		if !validChannels[c] {
			return nil, ErrInvalidChannel
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

// Create makes a new inbox. The first inbox in a workspace becomes the
// default automatically (bootstrap already guarantees one exists, so this
// only matters for a workspace's second inbox onward, which never is).
func (s *Service) Create(
	ctx context.Context, workspaceID, actorMemberID, name, slug string, description *string, channels, teamIDs []string,
) (*Inbox, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil, ErrInvalidSlug
	}
	normalizedChannels, err := normalizeChannels(channels)
	if err != nil {
		return nil, err
	}

	id := ids.New(ids.PrefixInbox)

	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.insert(ctx, tx, id, workspaceID, name, slug, description, normalizedChannels); err != nil {
			return err
		}
		if err := s.repo.setTeams(ctx, tx, id, teamIDs); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "inbox.created", EntityType: "inbox", EntityID: id,
			Metadata: map[string]any{"name": name},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "inbox.created",
			EntityType: "inbox", EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
	if err != nil {
		if errors.Is(err, errUniqueSlug) {
			return nil, ErrSlugTaken
		}
		return nil, err
	}

	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) Update(
	ctx context.Context, workspaceID, actorMemberID, id, name string, description *string, channels, teamIDs []string,
) (*Inbox, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	normalizedChannels, err := normalizeChannels(channels)
	if err != nil {
		return nil, err
	}

	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.update(ctx, tx, workspaceID, id, name, description, normalizedChannels); err != nil {
			return err
		}
		if err := s.repo.setTeams(ctx, tx, id, teamIDs); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "inbox.updated", EntityType: "inbox", EntityID: id,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: "inbox.updated",
			EntityType: "inbox", EntityID: id, ActorType: events.ActorUser, ActorID: actorMemberID,
		})
	})
	if err != nil {
		if errors.Is(err, errUniqueSlug) {
			return nil, ErrSlugTaken
		}
		return nil, err
	}

	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) SetDefault(ctx context.Context, workspaceID, actorMemberID, id string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.setDefault(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "inbox.default_changed", EntityType: "inbox", EntityID: id,
		})
	})
}

func (s *Service) Delete(ctx context.Context, workspaceID, actorMemberID, id string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.delete(ctx, tx, workspaceID, id); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "inbox.deleted", EntityType: "inbox", EntityID: id,
		})
	})
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Inbox, error) {
	return s.repo.byID(ctx, workspaceID, id)
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Inbox, error) {
	return s.repo.list(ctx, workspaceID)
}
