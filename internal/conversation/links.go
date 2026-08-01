package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrInvalidLinkRelation = errors.New("conversation: invalid link relation")
	ErrLinkToSelf          = errors.New("conversation: cannot link a conversation to itself")
	ErrLinkAlreadyExists   = errors.New("conversation: link already exists")
)

var validLinkRelations = map[string]bool{
	"related": true, "duplicate_of": true, "follow_up": true,
}

type ConversationLink struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	SourceID    string    `json:"source_id"`
	TargetID    string    `json:"target_id"`
	Relation    string    `json:"relation"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Links returns every relationship touching the conversation, regardless of
// which endpoint was stored as source. That makes the UI symmetric even for
// directional relations such as duplicate_of and follow_up.
func (s *Service) Links(ctx context.Context, workspaceID, conversationID string) ([]ConversationLink, error) {
	return s.LinksPage(ctx, workspaceID, conversationID, time.Time{}, "", 0)
}

// LinksPage returns relationships in stable newest-first order. The cursor is
// the created_at/id pair used by the API's opaque pagination envelope.
func (s *Service) LinksPage(ctx context.Context, workspaceID, conversationID string, before time.Time, beforeID string, limit int) ([]ConversationLink, error) {
	if _, err := s.repo.byID(ctx, workspaceID, conversationID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	where := "workspace_id=$1 AND (source_id=$2 OR target_id=$2)"
	args := []any{workspaceID, conversationID}
	if !before.IsZero() {
		where += " AND (created_at,id)<($3,$4)"
		args = append(args, before, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, source_id, target_id, relation, created_by, created_at
		FROM conversation_links
		WHERE `+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("conversation: list links: %w", err)
	}
	defer rows.Close()
	links := []ConversationLink{}
	for rows.Next() {
		var link ConversationLink
		if err := rows.Scan(&link.ID, &link.WorkspaceID, &link.SourceID, &link.TargetID, &link.Relation, &link.CreatedBy, &link.CreatedAt); err != nil {
			return nil, fmt.Errorf("conversation: scan link: %w", err)
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

// Link stores one relationship. Only the undirected "related" relationship
// is canonicalised; duplicate_of and follow_up retain their direction.
func (s *Service) Link(ctx context.Context, workspaceID, actorMemberID, sourceID, targetID, relation string) (*ConversationLink, error) {
	relation = strings.TrimSpace(strings.ToLower(relation))
	if !validLinkRelations[relation] {
		return nil, ErrInvalidLinkRelation
	}
	if sourceID == targetID {
		return nil, ErrLinkToSelf
	}
	if relation == "related" && sourceID > targetID {
		sourceID, targetID = targetID, sourceID
	}
	var created ConversationLink
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := ensureConversationInWorkspace(ctx, tx, workspaceID, sourceID); err != nil {
			return err
		}
		if err := ensureConversationInWorkspace(ctx, tx, workspaceID, targetID); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO conversation_links (id, workspace_id, source_id, target_id, relation, created_by)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (workspace_id,source_id,target_id,relation) DO NOTHING
			RETURNING id, workspace_id, source_id, target_id, relation, created_by, created_at
		`, ids.New(ids.PrefixConversationLink), workspaceID, sourceID, targetID, relation, actorMemberID).Scan(
			&created.ID, &created.WorkspaceID, &created.SourceID, &created.TargetID, &created.Relation, &created.CreatedBy, &created.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLinkAlreadyExists
		}
		if err != nil {
			return fmt.Errorf("conversation: create link: %w", err)
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.ConversationLinked, EntityType: "conversation", EntityID: sourceID,
			Metadata: map[string]any{"target_id": targetID, "relation": relation},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationLinked,
			EntityType: "conversation", EntityID: sourceID, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": created.ID, "source_id": sourceID, "target_id": targetID, "relation": relation},
		})
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *Service) Unlink(ctx context.Context, workspaceID, actorMemberID, conversationID, targetID, relation string) error {
	relation = strings.TrimSpace(strings.ToLower(relation))
	if !validLinkRelations[relation] {
		return ErrInvalidLinkRelation
	}
	sourceID := conversationID
	if relation == "related" && sourceID > targetID {
		sourceID, targetID = targetID, sourceID
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var linkID string
		err := tx.QueryRow(ctx, `
			DELETE FROM conversation_links
			WHERE workspace_id=$1 AND source_id=$2 AND target_id=$3 AND relation=$4
			RETURNING id
		`, workspaceID, sourceID, targetID, relation).Scan(&linkID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("conversation: delete link: %w", err)
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: audit.ConversationUnlinked, EntityType: "conversation", EntityID: conversationID,
			Metadata: map[string]any{"target_id": targetID, "relation": relation},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationUnlinked,
			EntityType: "conversation", EntityID: conversationID, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"id": linkID, "source_id": sourceID, "target_id": targetID, "relation": relation},
		})
	})
}

func ensureConversationInWorkspace(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}
