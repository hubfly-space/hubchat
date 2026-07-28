package customer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrMergeSelf            = errors.New("customer: cannot merge a customer into itself")
	ErrMergeNotFound        = errors.New("customer: merge record not found")
	ErrMergeAlreadyReversed = errors.New("customer: this merge was already reversed")
	ErrMergeWindowClosed    = errors.New("customer: the window to reverse this merge has closed")
)

// mergeReversibilityWindow is how long a merge stays undoable (§6.9). Chosen
// to match the "reversible for 30 days" copy already written into the
// dashboard's merge dialog before this module existed.
const mergeReversibilityWindow = 30 * 24 * time.Hour

// MergePreview is what the merge dialog shows before committing — the same
// counts moved_counts records afterward, computed without writing anything.
type MergePreview struct {
	ConversationCount int
	TicketCount       int
	TagCount          int
	CompanyCount      int
}

// MergeRecord mirrors identity_merge_history — the audit trail a merge and
// its (possible) reversal read and write.
type MergeRecord struct {
	ID              string
	WorkspaceID     string
	WinnerID        string
	LoserID         string
	MovedCounts     map[string]any
	MergedBy        *string
	ReversibleUntil *time.Time
	ReversedAt      *time.Time
	ReversedBy      *string
	CreatedAt       time.Time
}

func (r *repository) countConversations(ctx context.Context, workspaceID, customerID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE workspace_id = $1 AND customer_id = $2`, workspaceID, customerID).Scan(&n)
	return n, err
}

func (r *repository) countTickets(ctx context.Context, workspaceID, customerID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM tickets WHERE workspace_id = $1 AND customer_id = $2`, workspaceID, customerID).Scan(&n)
	return n, err
}

// lockCustomerRow takes a row lock for the duration of the merge transaction
// so a concurrent update to the loser cannot race the reassignment below it.
func (r *repository) lockCustomerRow(ctx context.Context, tx pgx.Tx, workspaceID, id string) (*Customer, error) {
	row := tx.QueryRow(ctx, `SELECT `+customerColumns+`
		FROM customers WHERE workspace_id = $1 AND id = $2 FOR UPDATE
	`, workspaceID, id)
	return scanCustomer(row)
}

func (r *repository) conversationIDs(ctx context.Context, tx pgx.Tx, workspaceID, customerID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM conversations WHERE workspace_id = $1 AND customer_id = $2`, workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIDs(rows)
}

func (r *repository) ticketIDs(ctx context.Context, tx pgx.Tx, workspaceID, customerID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM tickets WHERE workspace_id = $1 AND customer_id = $2`, workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIDs(rows)
}

func scanIDs(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]string, error) {
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *repository) reassignConversations(ctx context.Context, tx pgx.Tx, ids []string, winnerID string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE conversations SET customer_id = $1 WHERE id = ANY($2)`, winnerID, ids)
	return err
}

func (r *repository) reassignTickets(ctx context.Context, tx pgx.Tx, ids []string, winnerID string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE tickets SET customer_id = $1 WHERE id = ANY($2)`, winnerID, ids)
	return err
}

// reassignHistory moves the loser's high-volume, cascade-vulnerable
// children (events, sessions, notes) to the winner rather than losing them
// to `ON DELETE CASCADE` when the loser row is removed — a merge should
// enrich the surviving record's history, not erase it. Unlike conversations
// and tickets, these reassignments are not tracked for reversal (§6.9's
// 30-day undo covers the case-tracking objects; a chatty customer's page-view
// history is not worth the bookkeeping to move back precisely).
func (r *repository) reassignHistory(ctx context.Context, tx pgx.Tx, loserID, winnerID string) error {
	if _, err := tx.Exec(ctx, `UPDATE customer_events SET customer_id = $1 WHERE customer_id = $2`, winnerID, loserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE contact_sessions SET customer_id = $1 WHERE customer_id = $2`, winnerID, loserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE customer_notes SET customer_id = $1 WHERE customer_id = $2`, winnerID, loserID); err != nil {
		return err
	}
	return nil
}

func (r *repository) mergeTagsInto(ctx context.Context, tx pgx.Tx, winnerID, loserID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO customer_tags (customer_id, tag_id)
		SELECT $1, tag_id FROM customer_tags WHERE customer_id = $2
		ON CONFLICT DO NOTHING
	`, winnerID, loserID)
	return err
}

func (r *repository) mergeCompaniesInto(ctx context.Context, tx pgx.Tx, winnerID, loserID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO company_customers (company_id, customer_id)
		SELECT company_id, $1 FROM company_customers WHERE customer_id = $2
		ON CONFLICT DO NOTHING
	`, winnerID, loserID)
	return err
}

func (r *repository) deleteCustomerRow(ctx context.Context, tx pgx.Tx, workspaceID, id string) error {
	_, err := tx.Exec(ctx, `DELETE FROM customers WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	return err
}

// recreateCustomer restores a merged-away customer row from its snapshot on
// reversal. Every column customerColumns reads is written back so the
// restored row round-trips exactly.
func (r *repository) recreateCustomer(ctx context.Context, tx pgx.Tx, c Customer) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO customers
			(id, workspace_id, name, email, phone, avatar_url, external_id, verification,
			 language, timezone, attributes, owner_id, first_seen_at, last_seen_at, last_contacted_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`, c.ID, c.WorkspaceID, c.Name, c.Email, c.Phone, c.AvatarURL, c.ExternalID, c.Verification,
		c.Language, c.Timezone, c.Attributes, c.OwnerID, c.FirstSeenAt, c.LastSeenAt, c.LastContactedAt, c.Version)
	return err
}

func (r *repository) reassignConversationsBack(ctx context.Context, tx pgx.Tx, ids []string, loserID string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE conversations SET customer_id = $1 WHERE id = ANY($2)`, loserID, ids)
	return err
}

func (r *repository) reassignTicketsBack(ctx context.Context, tx pgx.Tx, ids []string, loserID string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE tickets SET customer_id = $1 WHERE id = ANY($2)`, loserID, ids)
	return err
}

func (r *repository) restoreTags(ctx context.Context, tx pgx.Tx, loserID string, tagIDs []string) error {
	for _, tagID := range tagIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO customer_tags (customer_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, loserID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func (r *repository) restoreCompanies(ctx context.Context, tx pgx.Tx, loserID string, companyIDs []string) error {
	for _, companyID := range companyIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO company_customers (company_id, customer_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, companyID, loserID); err != nil {
			return err
		}
	}
	return nil
}

func (r *repository) insertMergeHistory(
	ctx context.Context, tx pgx.Tx, id, workspaceID, winnerID, loserID string,
	snapshot, movedCounts map[string]any, mergedBy string, reversibleUntil time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO identity_merge_history
			(id, workspace_id, winner_id, loser_id, entity_type, loser_snapshot, moved_counts, merged_by, reversible_until)
		VALUES ($1, $2, $3, $4, 'customer', $5, $6, $7, $8)
	`, id, workspaceID, winnerID, loserID, snapshot, movedCounts, mergedBy, reversibleUntil)
	return err
}

func (r *repository) mergeHistoryByID(ctx context.Context, workspaceID, id string) (*MergeRecord, error) {
	var m MergeRecord
	var countsRaw []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, workspace_id, winner_id, loser_id, moved_counts,
		       merged_by, reversible_until, reversed_at, reversed_by, created_at
		FROM identity_merge_history WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id).Scan(
		&m.ID, &m.WorkspaceID, &m.WinnerID, &m.LoserID, &countsRaw,
		&m.MergedBy, &m.ReversibleUntil, &m.ReversedAt, &m.ReversedBy, &m.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMergeNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(countsRaw, &m.MovedCounts); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *repository) mergeSnapshot(ctx context.Context, workspaceID, id string) (map[string]any, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `
		SELECT loser_snapshot FROM identity_merge_history WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMergeNotFound
	}
	if err != nil {
		return nil, err
	}
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (r *repository) markMergeReversed(ctx context.Context, tx pgx.Tx, workspaceID, id, reversedBy string) error {
	_, err := tx.Exec(ctx, `
		UPDATE identity_merge_history SET reversed_at = now(), reversed_by = $3
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, id, reversedBy)
	return err
}

// ---------------------------------------------------------------- service

// PreviewMerge reports what merging loserID into winnerID would move,
// without writing anything — the merge dialog's confirmation step.
func (s *Service) PreviewMerge(ctx context.Context, workspaceID, winnerID, loserID string) (*MergePreview, error) {
	if winnerID == loserID {
		return nil, ErrMergeSelf
	}
	if _, err := s.repo.byID(ctx, workspaceID, winnerID); err != nil {
		return nil, err
	}
	if _, err := s.repo.byID(ctx, workspaceID, loserID); err != nil {
		return nil, err
	}

	convCount, err := s.repo.countConversations(ctx, workspaceID, loserID)
	if err != nil {
		return nil, err
	}
	ticketCount, err := s.repo.countTickets(ctx, workspaceID, loserID)
	if err != nil {
		return nil, err
	}
	tagIDs, err := s.repo.tagIDs(ctx, workspaceID, loserID)
	if err != nil {
		return nil, err
	}
	companyIDs, err := s.repo.companyIDs(ctx, workspaceID, loserID)
	if err != nil {
		return nil, err
	}

	return &MergePreview{
		ConversationCount: convCount, TicketCount: ticketCount,
		TagCount: len(tagIDs), CompanyCount: len(companyIDs),
	}, nil
}

// Merge absorbs loserID into winnerID (§6.9): conversations and tickets are
// reassigned (and tracked, for reversal); events/sessions/notes are
// reassigned permanently; tags and company links merge into the winner's
// set; the loser's full profile is snapshotted, then the row is removed.
func (s *Service) Merge(ctx context.Context, workspaceID, actorMemberID, winnerID, loserID string) (*MergeRecord, error) {
	if winnerID == loserID {
		return nil, ErrMergeSelf
	}

	// Lock in a fixed order (lexical on id) to avoid a deadlock against a
	// concurrent merge of the same pair in the opposite direction.
	firstID, secondID := winnerID, loserID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}

	mergeID := ids.New(ids.PrefixMerge)
	reversibleUntil := time.Now().Add(mergeReversibilityWindow)
	var movedCounts map[string]any

	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := s.repo.lockCustomerRow(ctx, tx, workspaceID, firstID); err != nil {
			return err
		}
		if _, err := s.repo.lockCustomerRow(ctx, tx, workspaceID, secondID); err != nil {
			return err
		}
		loser, err := s.repo.lockCustomerRow(ctx, tx, workspaceID, loserID)
		if err != nil {
			return err
		}

		convIDs, err := s.repo.conversationIDs(ctx, tx, workspaceID, loserID)
		if err != nil {
			return err
		}
		ticketIDs, err := s.repo.ticketIDs(ctx, tx, workspaceID, loserID)
		if err != nil {
			return err
		}
		tagIDs, err := s.repo.tagIDs(ctx, workspaceID, loserID)
		if err != nil {
			return err
		}
		companyIDs, err := s.repo.companyIDs(ctx, workspaceID, loserID)
		if err != nil {
			return err
		}

		if err := s.repo.reassignConversations(ctx, tx, convIDs, winnerID); err != nil {
			return err
		}
		if err := s.repo.reassignTickets(ctx, tx, ticketIDs, winnerID); err != nil {
			return err
		}
		if err := s.repo.reassignHistory(ctx, tx, loserID, winnerID); err != nil {
			return err
		}
		if err := s.repo.mergeTagsInto(ctx, tx, winnerID, loserID); err != nil {
			return err
		}
		if err := s.repo.mergeCompaniesInto(ctx, tx, winnerID, loserID); err != nil {
			return err
		}

		// customer is stored as the Customer struct itself (not a hand-built
		// map): Go's default JSON codec keys each field by its exact Go name
		// (Customer carries no json tags, since it is never otherwise
		// serialised), and reversal decodes with the same struct so the two
		// sides can only ever agree on the key spelling.
		snapshot := map[string]any{
			"customer":         *loser,
			"conversation_ids": convIDs,
			"ticket_ids":       ticketIDs,
			"tag_ids":          tagIDs,
			"company_ids":      companyIDs,
		}
		movedCounts = map[string]any{
			"conversations": len(convIDs), "tickets": len(ticketIDs), "tags": len(tagIDs), "companies": len(companyIDs),
		}

		if err := s.repo.deleteCustomerRow(ctx, tx, workspaceID, loserID); err != nil {
			return err
		}
		if err := s.repo.insertMergeHistory(ctx, tx, mergeID, workspaceID, winnerID, loserID, snapshot, movedCounts, actorMemberID, reversibleUntil); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "customer.merged", EntityType: "customer", EntityID: winnerID,
			Metadata: map[string]any{"loser_id": loserID, "moved": movedCounts},
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.CustomerMerged,
			EntityType: "customer", EntityID: winnerID, ActorType: events.ActorUser, ActorID: actorMemberID,
			Data: map[string]any{"winner_id": winnerID, "loser_id": loserID},
		})
	})
	if err != nil {
		return nil, err
	}

	return s.repo.mergeHistoryByID(ctx, workspaceID, mergeID)
}

// ReverseMerge restores the loser record and moves its conversations/tickets
// back, provided the reversibility window has not closed (§6.9).
func (s *Service) ReverseMerge(ctx context.Context, workspaceID, actorMemberID, mergeID string) error {
	record, err := s.repo.mergeHistoryByID(ctx, workspaceID, mergeID)
	if err != nil {
		return err
	}
	if record.ReversedAt != nil {
		return ErrMergeAlreadyReversed
	}
	if record.ReversibleUntil == nil || time.Now().After(*record.ReversibleUntil) {
		return ErrMergeWindowClosed
	}

	snapshot, err := s.repo.mergeSnapshot(ctx, workspaceID, mergeID)
	if err != nil {
		return err
	}

	customerRaw, err := json.Marshal(snapshot["customer"])
	if err != nil {
		return err
	}
	var loser Customer
	if err := json.Unmarshal(customerRaw, &loser); err != nil {
		return fmt.Errorf("customer: decode merge snapshot: %w", err)
	}

	convIDs := toStringSlice(snapshot["conversation_ids"])
	ticketIDs := toStringSlice(snapshot["ticket_ids"])
	tagIDs := toStringSlice(snapshot["tag_ids"])
	companyIDs := toStringSlice(snapshot["company_ids"])

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.recreateCustomer(ctx, tx, loser); err != nil {
			return err
		}
		if err := s.repo.reassignConversationsBack(ctx, tx, convIDs, record.LoserID); err != nil {
			return err
		}
		if err := s.repo.reassignTicketsBack(ctx, tx, ticketIDs, record.LoserID); err != nil {
			return err
		}
		if err := s.repo.restoreTags(ctx, tx, record.LoserID, tagIDs); err != nil {
			return err
		}
		if err := s.repo.restoreCompanies(ctx, tx, record.LoserID, companyIDs); err != nil {
			return err
		}
		if err := s.repo.markMergeReversed(ctx, tx, workspaceID, mergeID, actorMemberID); err != nil {
			return err
		}
		return s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: audit.ActorUser, ActorID: actorMemberID,
			Action: "customer.merge_reversed", EntityType: "customer", EntityID: record.WinnerID,
			Metadata: map[string]any{"restored_id": record.LoserID},
		})
	})
}

func toStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
