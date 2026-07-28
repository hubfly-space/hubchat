package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrInvalidTagName = errors.New("workspace: tag name is required")
	ErrInvalidColor   = errors.New("workspace: color must be between 1 and 6")
	ErrTagNameTaken   = errors.New("workspace: a tag with this name already exists")
	ErrTagNotFound    = errors.New("workspace: tag not found")
)

// tagJunctionTable is one table that references tags.id, paired with the
// column that names the tagged entity — which differs per table, and matters
// because a merge has to know it to tell "this entity already has the target
// tag" from "an unrelated entity happens to have it".
//
// Listed explicitly because SQL has no parameter for an identifier: neither
// the table name nor the column name can be bound, only chosen from a fixed,
// hardcoded set like this one.
type tagJunctionTable struct {
	table  string
	entity string
}

// Every module with a tags junction table today — conversations, tickets,
// customers, companies, feedback items, and knowledge-base articles all tag
// against the same pool (§6.1: tags are a workspace-wide taxonomy, not
// per-feature).
var tagJunctionTables = []tagJunctionTable{
	{"conversation_tags", "conversation_id"},
	{"ticket_tags", "ticket_id"},
	{"customer_tags", "customer_id"},
	{"company_tags", "company_id"},
	{"feedback_tags", "item_id"},
	{"article_tags", "article_id"},
}

// CreateTag adds a tag to the workspace's shared taxonomy.
func (s *Service) CreateTag(ctx context.Context, workspaceID, actorMemberID, name string, color int) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidTagName
	}
	if color < 1 || color > 6 {
		return nil, ErrInvalidColor
	}

	id := ids.New(ids.PrefixTag)
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.insertTag(ctx, tx, id, workspaceID, name, color); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "tag.created", EntityType: "tag", EntityID: id,
			Metadata: map[string]any{"name": name},
		})
	})
	if err != nil {
		if errors.Is(err, errUniqueTagName) {
			return nil, ErrTagNameTaken
		}
		return nil, err
	}
	return &Tag{ID: id, Name: name, Color: color}, nil
}

// ListTags returns every tag in the workspace with its usage count, for the
// tags settings screen. Distinct from the bootstrap payload's own tag list
// (repository.listTags), which skips the usage join deliberately — the
// opening screen has no use for that number, and computing it there would
// cost every cold load a scan over six tagging tables for a figure nothing on
// that screen shows.
func (s *Service) ListTags(ctx context.Context, workspaceID string) ([]Tag, error) {
	return s.repo.listTagsWithUsage(ctx, workspaceID)
}

// DeleteTag removes a tag outright. Every reference to it across every
// tagging table is removed by the ON DELETE CASCADE foreign keys the owning
// migrations declare — there is nothing else for this method to clean up.
func (s *Service) DeleteTag(ctx context.Context, workspaceID, actorMemberID, tagID string) error {
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := s.repo.deleteTag(ctx, tx, workspaceID, tagID)
		if err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "tag.deleted", EntityType: "tag", EntityID: tagID,
			Metadata: map[string]any{"name": tag.Name},
		})
	})
}

// MergeTags reassigns every use of source to target across every tagging
// table, then removes source.
//
// This is the operation Tags.tsx's "Merge" button needs: two tags that turned
// out to mean the same thing collapse into one without anyone having to
// manually re-tag every conversation, ticket, and customer that used the old
// one.
func (s *Service) MergeTags(ctx context.Context, workspaceID, actorMemberID, sourceTagID, targetTagID string) error {
	if sourceTagID == targetTagID {
		return errors.New("workspace: cannot merge a tag into itself")
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		source, err := s.repo.lockTag(ctx, tx, workspaceID, sourceTagID)
		if err != nil {
			return err
		}
		if _, err := s.repo.lockTag(ctx, tx, workspaceID, targetTagID); err != nil {
			return err
		}

		for _, junction := range tagJunctionTables {
			if err := s.repo.reassignTagUsage(ctx, tx, junction, sourceTagID, targetTagID); err != nil {
				return err
			}
		}

		if _, err := s.repo.deleteTag(ctx, tx, workspaceID, sourceTagID); err != nil {
			return err
		}

		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "tag.merged", EntityType: "tag", EntityID: targetTagID,
			Metadata: map[string]any{"merged_name": source.Name, "into": targetTagID},
		})
	})
}

// ---------------------------------------------------------------- repository

var errUniqueTagName = errors.New("workspace: duplicate tag name")

func (r *repository) insertTag(ctx context.Context, tx pgx.Tx, id, workspaceID, name string, color int) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tags (id, workspace_id, name, color) VALUES ($1, $2, $3, $4)
	`, id, workspaceID, name, color)
	if err != nil && isUniqueViolation(err) {
		return errUniqueTagName
	}
	if err != nil {
		return fmt.Errorf("workspace: insert tag: %w", err)
	}
	return nil
}

func (r *repository) listTagsWithUsage(ctx context.Context, workspaceID string) ([]Tag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT t.id, t.name::text, t.color,
		       coalesce((
		           SELECT sum(c) FROM (
		               SELECT count(*) c FROM conversation_tags WHERE tag_id = t.id
		               UNION ALL SELECT count(*) FROM ticket_tags WHERE tag_id = t.id
		               UNION ALL SELECT count(*) FROM customer_tags WHERE tag_id = t.id
		               UNION ALL SELECT count(*) FROM company_tags WHERE tag_id = t.id
		               UNION ALL SELECT count(*) FROM feedback_tags WHERE tag_id = t.id
		               UNION ALL SELECT count(*) FROM article_tags WHERE tag_id = t.id
		           ) usage
		       ), 0) AS usage_count
		FROM tags t
		WHERE t.workspace_id = $1
		ORDER BY t.name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace: list tags: %w", err)
	}
	defer rows.Close()

	out := []Tag{}
	for rows.Next() {
		var t Tag
		var usage int64
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &usage); err != nil {
			return nil, err
		}
		t.UsageCount = int(usage)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *repository) lockTag(ctx context.Context, tx pgx.Tx, workspaceID, tagID string) (*Tag, error) {
	var t Tag
	err := tx.QueryRow(ctx, `
		SELECT id, name::text, color FROM tags
		WHERE workspace_id = $1 AND id = $2
		FOR UPDATE
	`, workspaceID, tagID).Scan(&t.ID, &t.Name, &t.Color)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTagNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: lock tag: %w", err)
	}
	return &t, nil
}

func (r *repository) deleteTag(ctx context.Context, tx pgx.Tx, workspaceID, tagID string) (*Tag, error) {
	var t Tag
	err := tx.QueryRow(ctx, `
		DELETE FROM tags WHERE workspace_id = $1 AND id = $2
		RETURNING id, name::text, color
	`, workspaceID, tagID).Scan(&t.ID, &t.Name, &t.Color)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTagNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: delete tag: %w", err)
	}
	return &t, nil
}

// reassignTagUsage moves every row in one junction table from source to
// target.
//
// Two steps, in this order:
//
//  1. Delete source-tag rows whose entity already carries the target tag.
//     A junction table's primary key is (entity_column, tag_id), so an
//     entity tagged with *both* source and target would make the plain
//     UPDATE below violate that key — this removes the would-be duplicate
//     first, on the entity column named for this specific table (the whole
//     reason the caller passes it rather than this function guessing).
//  2. Reassign everything that is left from source to target.
//
// table and entityColumn are never user input; see tagJunctionTables.
func (r *repository) reassignTagUsage(ctx context.Context, tx pgx.Tx, junction tagJunctionTable, sourceTagID, targetTagID string) error {
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %[1]s a
		USING %[1]s b
		WHERE a.tag_id = $1 AND b.tag_id = $2
		  AND a.%[2]s = b.%[2]s
	`, junction.table, junction.entity), sourceTagID, targetTagID); err != nil {
		return fmt.Errorf("workspace: merge tag usage in %s: %w", junction.table, err)
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET tag_id = $2 WHERE tag_id = $1
	`, junction.table), sourceTagID, targetTagID); err != nil {
		return fmt.Errorf("workspace: reassign tag usage in %s: %w", junction.table, err)
	}
	return nil
}
