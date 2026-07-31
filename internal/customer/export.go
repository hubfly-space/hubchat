package customer

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
)

// ExportBundle is the JSON document a per-customer data export produces —
// GDPR/CCPA data portability (§12), everything Hubchat holds about one
// person in one file.
type ExportBundle struct {
	Customer      Customer         `json:"customer"`
	TagIDs        []string         `json:"tag_ids"`
	CompanyIDs    []string         `json:"company_ids"`
	Conversations []ExportRef      `json:"conversations"`
	Tickets       []ExportRef      `json:"tickets"`
	Events        []CustomerEvent  `json:"events"`
	Sessions      []ContactSession `json:"sessions"`
}

// ExportRef is the minimal pointer an export needs into another module's
// records — an id and a label, not the full conversation/ticket (those stay
// queryable through the product; the export is about *this person's data*,
// not a replacement for the inbox or ticket views).
type ExportRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func (r *repository) exportRefs(ctx context.Context, table, labelExpr, workspaceID, customerID string) ([]ExportRef, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, `+labelExpr+` FROM `+table+` WHERE workspace_id = $1 AND customer_id = $2`,
		workspaceID, customerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ExportRef{}
	for rows.Next() {
		var ref ExportRef
		if err := rows.Scan(&ref.ID, &ref.Label); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// Export assembles the full export bundle for one customer (§12 data
// portability). Read-only — nothing here is audited as a mutation, but the
// handler that serves it records the access.
func (s *Service) Export(ctx context.Context, workspaceID, customerID string) (*ExportBundle, error) {
	customer, err := s.repo.byID(ctx, workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	tagIDs, err := s.repo.tagIDs(ctx, workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	companyIDs, err := s.repo.companyIDs(ctx, workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	conversations, err := s.repo.exportRefs(ctx, "conversations", "coalesce(subject, last_message_preview, '')", workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	tickets, err := s.repo.exportRefs(ctx, "tickets", "title", workspaceID, customerID)
	if err != nil {
		return nil, err
	}
	eventList, err := s.repo.timelineByCustomer(ctx, workspaceID, customerID, time.Time{}, "", 500)
	if err != nil {
		return nil, err
	}
	sessions, err := s.repo.sessionsByCustomer(ctx, workspaceID, customerID, 100)
	if err != nil {
		return nil, err
	}

	return &ExportBundle{
		Customer: *customer, TagIDs: tagIDs, CompanyIDs: companyIDs,
		Conversations: conversations, Tickets: tickets, Events: eventList, Sessions: sessions,
	}, nil
}

// Delete anonymises a customer's identifying fields and erases their
// event/session/note history, but keeps the row (and the conversations and
// tickets pointing at it) intact — §12's erasure right applies to personal
// data, not to the support history a business is separately entitled to keep
// for its own records. This is what the "Delete and anonymise" action means:
// a hard row delete would instead null out every conversation/ticket's
// customer_id (ON DELETE SET NULL) and quietly sever the link this exists to
// preserve.
func (s *Service) Delete(ctx context.Context, workspaceID, actorMemberID, customerID string) error {
	return s.delete(ctx, workspaceID, audit.ActorUser, events.ActorUser, actorMemberID, customerID)
}

// DeleteAsCustomer applies the same anonymisation policy when the request
// comes from an authenticated customer portal session. Support history stays
// available to the workspace, while personal identifiers and customer-owned
// context are erased.
func (s *Service) DeleteAsCustomer(ctx context.Context, workspaceID, customerID string) error {
	return s.delete(ctx, workspaceID, audit.ActorCustomer, events.ActorCustomer, customerID, customerID)
}

func (s *Service) delete(ctx context.Context, workspaceID string, auditActor audit.ActorType, eventActor events.ActorType, actorID, customerID string) error {
	if _, err := s.repo.byID(ctx, workspaceID, customerID); err != nil {
		return err
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.anonymize(ctx, tx, workspaceID, customerID); err != nil {
			return err
		}
		if err := s.repo.eraseHistory(ctx, tx, workspaceID, customerID); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, tx, audit.Entry{
			WorkspaceID: workspaceID, ActorType: auditActor, ActorID: actorID,
			Action: "customer.deleted", EntityType: "customer", EntityID: customerID,
		}); err != nil {
			return err
		}
		return s.appendEvent(ctx, tx, events.Event{
			WorkspaceID: workspaceID, Type: events.CustomerUpdated,
			EntityType: "customer", EntityID: customerID, ActorType: eventActor, ActorID: actorID,
			Data: map[string]any{"id": customerID, "anonymised": true},
		})
	})
}

func (r *repository) anonymize(ctx context.Context, tx pgx.Tx, workspaceID, customerID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE customers
		SET name = NULL, email = NULL, phone = NULL, external_id = NULL, avatar_url = NULL,
		    attributes = '{}'::jsonb, verification = 'anonymous', version = version + 1
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, customerID)
	return err
}

func (r *repository) eraseHistory(ctx context.Context, tx pgx.Tx, workspaceID, customerID string) error {
	// Credentials, alternate identities, and portal delivery preferences are
	// personal data even after the primary customer row is anonymised.
	for _, statement := range []string{
		`DELETE FROM portal_sessions WHERE workspace_id = $1 AND customer_id = $2`,
		`DELETE FROM portal_access_tokens WHERE workspace_id = $1 AND customer_id = $2`,
		`DELETE FROM portal_identities WHERE workspace_id = $1 AND customer_id = $2`,
		`DELETE FROM customer_emails WHERE workspace_id = $1 AND customer_id = $2`,
		`DELETE FROM customer_phones WHERE workspace_id = $1 AND customer_id = $2`,
		`DELETE FROM visitor_customer_links WHERE customer_id = $2 AND visitor_id IN (SELECT id FROM visitors WHERE workspace_id = $1)`,
		`DELETE FROM customer_notification_preferences WHERE workspace_id = $1 AND customer_id = $2`,
		`DELETE FROM feedback_votes WHERE workspace_id = $1 AND customer_id = $2`,
		`DELETE FROM feedback_subscriptions WHERE customer_id = $2 AND item_id IN (SELECT id FROM feedback_items WHERE workspace_id = $1)`,
	} {
		if _, err := tx.Exec(ctx, statement, workspaceID, customerID); err != nil {
			return err
		}
	}

	// Keep the support record's shape for agents, but remove customer-authored
	// prose and denormalised identity fields. Revisions are an independent copy
	// of message bodies and must be removed before the redaction is complete.
	if _, err := tx.Exec(ctx, `
		DELETE FROM message_revisions
		WHERE message_id IN (
			SELECT id FROM messages
			WHERE workspace_id = $1 AND author_type = 'customer' AND author_id = $2
		)
	`, workspaceID, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE messages
		SET author_id = NULL,
		    author_name = 'Deleted customer',
		    body = '[Message removed by customer request]',
		    redacted_at = coalesce(redacted_at, now()),
		    edited_at = now()
		WHERE workspace_id = $1 AND author_type = 'customer' AND author_id = $2
	`, workspaceID, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE feedback_comments
		SET author_id = NULL, author_name = 'Deleted customer',
		    body = '[Comment removed by customer request]', updated_at = now()
		WHERE workspace_id = $1 AND author_type = 'customer' AND author_id = $2
	`, workspaceID, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE article_feedback
		SET customer_id = NULL, comment = NULL
		WHERE workspace_id = $1 AND customer_id = $2
	`, workspaceID, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE survey_responses
		SET customer_id = NULL, comment = NULL
		WHERE workspace_id = $1 AND customer_id = $2
	`, workspaceID, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE form_submissions
		SET customer_id = NULL
		WHERE workspace_id = $1 AND customer_id = $2
	`, workspaceID, customerID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM customer_events WHERE workspace_id = $1 AND customer_id = $2`, workspaceID, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM contact_sessions WHERE workspace_id = $1 AND customer_id = $2`, workspaceID, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM customer_notes WHERE workspace_id = $1 AND customer_id = $2`, workspaceID, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM customer_tags WHERE customer_id = $1`, customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM company_customers WHERE customer_id = $1`, customerID); err != nil {
		return err
	}
	return nil
}
