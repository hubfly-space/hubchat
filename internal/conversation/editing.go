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
	ErrMessageNotFound     = errors.New("conversation: message not found")
	ErrNotMessageAuthor    = errors.New("conversation: only the original author may edit this message")
	ErrAlreadyRedacted     = errors.New("conversation: message already redacted")
	ErrCannotMergeIntoSelf = errors.New("conversation: cannot merge a conversation into itself")
)

// loadMessageForEdit loads the fields EditMessage and RedactMessage both need
// to authorize and record what they change, scoped to the conversation so a
// message id from another conversation cannot be targeted by guessing.
func (r *repository) loadMessageForEdit(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID string) (*Message, error) {
	row := tx.QueryRow(ctx, `SELECT `+messageColumns+`
		FROM messages
		WHERE workspace_id = $1 AND conversation_id = $2 AND id = $3
		FOR UPDATE
	`, workspaceID, conversationID, messageID)
	m, err := scanMessage(row)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrMessageNotFound
	}
	return m, nil
}

func (r *repository) insertRevision(ctx context.Context, tx pgx.Tx, id, messageID, body, editedBy string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO message_revisions (id, message_id, body, edited_by) VALUES ($1, $2, $3, $4)
	`, id, messageID, body, nullIfEmpty(editedBy))
	return err
}

func (r *repository) updateMessageBody(ctx context.Context, tx pgx.Tx, messageID, body string) error {
	_, err := tx.Exec(ctx, `
		UPDATE messages SET body = $2, edited_at = now() WHERE id = $1
	`, messageID, body)
	return err
}

func (r *repository) redactMessage(ctx context.Context, tx pgx.Tx, messageID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE messages SET body = '', redacted_at = now() WHERE id = $1
	`, messageID)
	return err
}

// EditMessage changes the body of a reply or note. Only the original author
// may edit — unlike redact, which is a moderation action anyone with the
// capability can take on anyone's message, an edit is the author correcting
// their own words, not a third party rewriting what someone else said.
func (s *Service) EditMessage(
	ctx context.Context, workspaceID, actorMemberID, conversationID, messageID, body string,
) (*Message, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, ErrEmptyBody
	}

	var result *Message
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		msg, err := s.repo.loadMessageForEdit(ctx, tx, workspaceID, conversationID, messageID)
		if err != nil {
			return err
		}
		if msg.Kind == "event" {
			return ErrMessageNotFound
		}
		if msg.AuthorType != "agent" || msg.AuthorID == nil || *msg.AuthorID != actorMemberID {
			return ErrNotMessageAuthor
		}

		if err := s.repo.insertRevision(ctx, tx, ids.New(ids.PrefixMessageRevision), messageID, msg.Body, actorMemberID); err != nil {
			return err
		}
		if err := s.repo.updateMessageBody(ctx, tx, messageID, body); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "message.edited", EntityType: entityConversation, EntityID: conversationID,
		}); err != nil {
			return err
		}
		if err := s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.MessageEdited,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "message_id": messageID, "body": body},
		}); err != nil {
			return err
		}

		now := time.Now().UTC()
		msg.Body = body
		msg.EditedAt = &now
		result = msg
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// RedactMessage clears a message's body, keeping the row (and its sequence
// position in the timeline) but removing the content — for the one case an
// edit does not cover: content that should not have been sent at all
// (§6.2), which any agent with conversation.delete may act on regardless of
// who wrote it. The original body survives only in message_revisions, which
// only reaches the audit trail, never the API.
func (s *Service) RedactMessage(ctx context.Context, workspaceID, actorMemberID, conversationID, messageID string) (*Message, error) {
	var result *Message
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		msg, err := s.repo.loadMessageForEdit(ctx, tx, workspaceID, conversationID, messageID)
		if err != nil {
			return err
		}
		if msg.Kind == "event" {
			return ErrMessageNotFound
		}

		if err := s.repo.insertRevision(ctx, tx, ids.New(ids.PrefixMessageRevision), messageID, msg.Body, actorMemberID); err != nil {
			return err
		}
		if err := s.repo.redactMessage(ctx, tx, messageID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "message.redacted", EntityType: entityConversation, EntityID: conversationID,
		}); err != nil {
			return err
		}
		if err := s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.MessageRedacted,
			EntityType: entityConversation, EntityID: conversationID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": conversationID, "message_id": messageID},
		}); err != nil {
			return err
		}

		now := time.Now().UTC()
		msg.Body = ""
		msg.RedactedAt = &now
		result = msg
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Merge moves every message from sourceID into targetID, renumbering them to
// continue targetID's own sequence, then closes the source conversation.
// Both must already exist in workspaceID — merging across tenants is not a
// permission gap, it is a category error the workspace predicate rules out
// entirely.
func (s *Service) Merge(ctx context.Context, workspaceID, actorMemberID, sourceID, targetID string) error {
	if sourceID == targetID {
		return ErrCannotMergeIntoSelf
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		// Fixed lock order (lexical on id) regardless of which is "source" and
		// which is "target" in the request, so two merges racing on the same
		// pair of conversations from opposite directions cannot deadlock.
		firstID, secondID := sourceID, targetID
		if secondID < firstID {
			firstID, secondID = secondID, firstID
		}
		if _, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, firstID); err != nil {
			return err
		}
		if _, err := s.repo.lockAndLoadFull(ctx, tx, workspaceID, secondID); err != nil {
			return err
		}

		moved, newPreview, newLastAt, err := s.repo.moveMessages(ctx, tx, sourceID, targetID)
		if err != nil {
			return err
		}
		if err := s.repo.applyMerge(ctx, tx, targetID, moved, newPreview, newLastAt); err != nil {
			return err
		}
		if err := s.repo.setState(ctx, tx, sourceID, "closed"); err != nil {
			return err
		}
		if err := s.repo.insertStatusHistory(ctx, tx, ids.New(ids.PrefixStatusHistory), sourceID, "", "closed", "member", actorMemberID); err != nil {
			return err
		}

		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "conversation.merged", EntityType: entityConversation, EntityID: targetID,
			Metadata: map[string]any{"source_id": sourceID, "messages_moved": moved},
		}); err != nil {
			return err
		}
		if err := s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationMerged,
			EntityType: entityConversation, EntityID: targetID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"target_id": targetID, "source_id": sourceID, "messages_moved": moved},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.ConversationStateSet,
			EntityType: entityConversation, EntityID: sourceID,
			ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"conversation_id": sourceID, "to": "closed", "merged_into": targetID},
		})
	})
}

// moveMessages re-sequences source's messages onto the end of target's own
// sequence in one statement, so a concurrent send on target cannot land in
// the middle of the range being renumbered.
func (r *repository) moveMessages(ctx context.Context, tx pgx.Tx, sourceID, targetID string) (moved int, lastPreview string, lastAt time.Time, err error) {
	rows, err := tx.Query(ctx, `
		WITH target_base AS (
			SELECT coalesce(max(sequence), 0) AS base FROM messages WHERE conversation_id = $2
		),
		renumbered AS (
			SELECT id, row_number() OVER (ORDER BY sequence) AS rn
			FROM messages WHERE conversation_id = $1
		)
		UPDATE messages m
		SET conversation_id = $2, sequence = target_base.base + renumbered.rn
		FROM renumbered, target_base
		WHERE m.id = renumbered.id
		RETURNING m.body, m.created_at
	`, sourceID, targetID)
	if err != nil {
		return 0, "", time.Time{}, fmt.Errorf("conversation: move messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var body string
		var createdAt time.Time
		if err := rows.Scan(&body, &createdAt); err != nil {
			return 0, "", time.Time{}, err
		}
		moved++
		if createdAt.After(lastAt) {
			lastAt = createdAt
			lastPreview = preview(body)
		}
	}
	return moved, lastPreview, lastAt, rows.Err()
}

func (r *repository) applyMerge(ctx context.Context, tx pgx.Tx, targetID string, moved int, newPreview string, newLastAt time.Time) error {
	if moved == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE conversations
		SET message_count = message_count + $2,
		    last_message_preview = CASE WHEN $4 > last_message_at THEN $3 ELSE last_message_preview END,
		    last_message_at = GREATEST(last_message_at, $4)
		WHERE id = $1
	`, targetID, moved, newPreview, newLastAt)
	return err
}

// Transcript renders a plain-text export of every message in a conversation,
// oldest first — the format §6.2's "export transcript" produces. Internal
// notes are marked rather than omitted: an exported transcript is for the
// workspace's own records, not the customer, so hiding notes from it would
// hide exactly the context a reviewer most needs.
func (s *Service) Transcript(ctx context.Context, workspaceID, conversationID string) (string, error) {
	conv, err := s.repo.byID(ctx, workspaceID, conversationID)
	if err != nil {
		return "", err
	}
	messages, err := s.repo.listMessages(ctx, workspaceID, conversationID, 0)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	subject := "(no subject)"
	if conv.Subject != nil && *conv.Subject != "" {
		subject = *conv.Subject
	}
	fmt.Fprintf(&b, "Conversation %s — %s\n", conv.ID, subject)
	fmt.Fprintf(&b, "Channel: %s | State: %s | Exported: %s\n", conv.Channel, conv.State, time.Now().UTC().Format(time.RFC3339))
	b.WriteString(strings.Repeat("-", 60) + "\n\n")

	for _, m := range messages {
		if m.Kind == "event" {
			continue
		}
		label := m.AuthorName
		if m.Kind == "note" {
			label += " (internal note)"
		}
		fmt.Fprintf(&b, "[%s] %s:\n", m.CreatedAt.UTC().Format(time.RFC3339), label)
		if m.RedactedAt != nil {
			b.WriteString("  (message redacted)\n\n")
			continue
		}
		for _, line := range strings.Split(m.Body, "\n") {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("\n")
	}

	return b.String(), nil
}
