// Package portability owns the workspace archive contract used by the CLI.
// It deliberately stores JSON rows rather than a database dump: an archive is
// inspectable, versioned, and can be imported into another workspace without
// granting the importer access to unrelated tenant rows.
package portability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/jackc/pgx/v5"
)

const CurrentVersion = 1

type Archive struct {
	Version           int                          `json:"version"`
	SourceWorkspaceID string                       `json:"source_workspace_id"`
	Workspace         map[string]any               `json:"workspace"`
	ExportedAt        time.Time                    `json:"exported_at"`
	Tables            map[string][]json.RawMessage `json:"tables"`
}

type TableSummary struct {
	Name     string `json:"name"`
	Rows     int    `json:"rows"`
	Existing int    `json:"existing,omitempty"`
	New      int    `json:"new,omitempty"`
}

var tableSpecs = []struct {
	name  string
	where string
	// direct means the archive row has a workspace_id that can be remapped.
	direct bool
}{
	{name: "inboxes", where: "workspace_id=$1", direct: true},
	{name: "teams", where: "workspace_id=$1", direct: true},
	{name: "companies", where: "workspace_id=$1", direct: true},
	{name: "customers", where: "workspace_id=$1", direct: true},
	{name: "customer_notification_preferences", where: "workspace_id=$1", direct: true},
	{name: "customer_emails", where: "workspace_id=$1", direct: true},
	{name: "customer_phones", where: "workspace_id=$1", direct: true},
	{name: "contact_sessions", where: "workspace_id=$1", direct: true},
	{name: "customer_events", where: "workspace_id=$1", direct: true},
	{name: "customer_notes", where: "workspace_id=$1", direct: true},
	{name: "attribute_definitions", where: "workspace_id=$1", direct: true},
	{name: "attribute_blocklist", where: "workspace_id=$1", direct: true},
	{name: "identity_merge_history", where: "workspace_id=$1", direct: true},
	{name: "blocked_contacts", where: "workspace_id=$1", direct: true},
	{name: "visitors", where: "workspace_id=$1", direct: true},
	{name: "conversations", where: "workspace_id=$1", direct: true},
	{name: "tags", where: "workspace_id=$1", direct: true},
	{name: "workspace_event_sequences", where: "workspace_id=$1", direct: true},
	{name: "tickets", where: "workspace_id=$1", direct: true},
	{name: "field_definitions", where: "workspace_id=$1", direct: true},
	{name: "saved_views", where: "workspace_id=$1", direct: true},
	{name: "macros", where: "workspace_id=$1", direct: true},
	{name: "saved_replies", where: "workspace_id=$1", direct: true},
	{name: "widgets", where: "workspace_id=$1", direct: true},
	{name: "portals", where: "workspace_id=$1", direct: true},
	{name: "portal_identities", where: "workspace_id=$1", direct: true},
	{name: "portal_sessions", where: "workspace_id=$1", direct: true},
	{name: "portal_access_tokens", where: "workspace_id=$1", direct: true},
	{name: "announcements", where: "workspace_id=$1", direct: true},
	{name: "forms", where: "workspace_id=$1", direct: true},
	{name: "feedback_boards", where: "workspace_id=$1", direct: true},
	{name: "feedback_items", where: "workspace_id=$1", direct: true},
	{name: "feedback_comments", where: "workspace_id=$1", direct: true},
	{name: "feedback_links", where: "workspace_id=$1", direct: true},
	{name: "knowledge_bases", where: "workspace_id=$1", direct: true},
	{name: "article_collections", where: "workspace_id=$1", direct: true},
	{name: "articles", where: "workspace_id=$1", direct: true},
	{name: "article_feedback", where: "workspace_id=$1", direct: true},
	{name: "article_searches", where: "workspace_id=$1", direct: true},
	{name: "changelog_entries", where: "workspace_id=$1", direct: true},
	{name: "surveys", where: "workspace_id=$1", direct: true},
	{name: "business_hour_calendars", where: "workspace_id=$1", direct: true},
	{name: "sla_policies", where: "workspace_id=$1", direct: true},
	{name: "sla_instances", where: "workspace_id=$1", direct: true},
	{name: "automation_rules", where: "workspace_id=$1", direct: true},
	{name: "automation_executions", where: "workspace_id=$1", direct: true},
	{name: "scheduled_actions", where: "workspace_id=$1", direct: true},
	{name: "tasks", where: "workspace_id=$1", direct: true},
	{name: "saved_reports", where: "workspace_id=$1", direct: true},
	{name: "report_schedules", where: "workspace_id=$1", direct: true},
	{name: "usage_counters", where: "workspace_id=$1", direct: true},
	{name: "workspace_limits", where: "workspace_id=$1", direct: true},
	{name: "api_keys", where: "workspace_id=$1", direct: true},
	{name: "webhook_endpoints", where: "workspace_id=$1", direct: true},
	{name: "webhook_deliveries", where: "workspace_id=$1", direct: true},
	{name: "integration_connections", where: "workspace_id=$1", direct: true},
	{name: "email_mailboxes", where: "workspace_id=$1", direct: true},
	{name: "email_messages", where: "workspace_id=$1", direct: true},
	{name: "email_delivery_events", where: "workspace_id=$1", direct: true},
	{name: "email_suppressions", where: "workspace_id=$1", direct: true},
	{name: "files", where: "workspace_id=$1", direct: true},
	{name: "workspace_events", where: "workspace_id=$1", direct: true},
	{name: "audit_logs", where: "workspace_id=$1", direct: true},
	{name: "notifications", where: "workspace_id=$1", direct: true},
	{name: "notification_preferences", where: "workspace_id=$1", direct: true},
	{name: "report_rollups", where: "workspace_id=$1", direct: true},
	{name: "report_rollup_state", where: "workspace_id=$1", direct: true},
	{name: "feature_flags", where: "workspace_id=$1", direct: true},
	{name: "team_routing_cursors", where: "team_id IN (SELECT id FROM teams WHERE workspace_id=$1)"},
	{name: "inbox_teams", where: "inbox_id IN (SELECT id FROM inboxes WHERE workspace_id=$1)"},
	{name: "widget_config_versions", where: "widget_id IN (SELECT id FROM widgets WHERE workspace_id=$1)"},
	{name: "widget_domains", where: "widget_id IN (SELECT id FROM widgets WHERE workspace_id=$1)"},
	{name: "portal_domains", where: "portal_id IN (SELECT id FROM portals WHERE workspace_id=$1)"},
	{name: "portal_navigation_items", where: "portal_id IN (SELECT id FROM portals WHERE workspace_id=$1)"},
	{name: "company_customers", where: "company_id IN (SELECT id FROM companies WHERE workspace_id=$1)"},
	{name: "visitor_customer_links", where: "visitor_id IN (SELECT id FROM visitors WHERE workspace_id=$1)"},
	{name: "customer_tags", where: "customer_id IN (SELECT id FROM customers WHERE workspace_id=$1)"},
	{name: "company_tags", where: "company_id IN (SELECT id FROM companies WHERE workspace_id=$1)"},
	{name: "conversation_participants", where: "conversation_id IN (SELECT id FROM conversations WHERE workspace_id=$1)"},
	{name: "conversation_followers", where: "conversation_id IN (SELECT id FROM conversations WHERE workspace_id=$1)"},
	{name: "composer_drafts", where: "conversation_id IN (SELECT id FROM conversations WHERE workspace_id=$1)"},
	{name: "messages", where: "conversation_id IN (SELECT id FROM conversations WHERE workspace_id=$1)"},
	{name: "message_revisions", where: "message_id IN (SELECT m.id FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE c.workspace_id=$1)"},
	{name: "message_reads", where: "message_id IN (SELECT m.id FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE c.workspace_id=$1)"},
	{name: "conversation_tags", where: "conversation_id IN (SELECT id FROM conversations WHERE workspace_id=$1)"},
	{name: "conversation_status_history", where: "conversation_id IN (SELECT id FROM conversations WHERE workspace_id=$1)"},
	{name: "ticket_tags", where: "ticket_id IN (SELECT id FROM tickets WHERE workspace_id=$1)"},
	{name: "ticket_links", where: "workspace_id=$1", direct: true},
	{name: "ticket_status_history", where: "ticket_id IN (SELECT id FROM tickets WHERE workspace_id=$1)"},
	{name: "ticket_followers", where: "ticket_id IN (SELECT id FROM tickets WHERE workspace_id=$1)"},
	{name: "field_values", where: "workspace_id=$1", direct: true},
	{name: "calendar_holidays", where: "calendar_id IN (SELECT id FROM business_hour_calendars WHERE workspace_id=$1)"},
	{name: "sla_policy_targets", where: "policy_id IN (SELECT id FROM sla_policies WHERE workspace_id=$1)"},
	{name: "automation_rule_versions", where: "rule_id IN (SELECT id FROM automation_rules WHERE workspace_id=$1)"},
	{name: "form_fields", where: "form_id IN (SELECT id FROM forms WHERE workspace_id=$1)"},
	{name: "form_submissions", where: "workspace_id=$1", direct: true},
	{name: "form_submission_values", where: "submission_id IN (SELECT id FROM form_submissions WHERE workspace_id=$1)"},
	{name: "survey_questions", where: "survey_id IN (SELECT id FROM surveys WHERE workspace_id=$1)"},
	{name: "survey_responses", where: "workspace_id=$1", direct: true},
	{name: "survey_answers", where: "response_id IN (SELECT id FROM survey_responses WHERE workspace_id=$1)"},
	{name: "feedback_votes", where: "workspace_id=$1", direct: true},
	{name: "feedback_subscriptions", where: "item_id IN (SELECT id FROM feedback_items WHERE workspace_id=$1)"},
	{name: "feedback_status_history", where: "item_id IN (SELECT id FROM feedback_items WHERE workspace_id=$1)"},
	{name: "feedback_tags", where: "item_id IN (SELECT id FROM feedback_items WHERE workspace_id=$1)"},
	{name: "article_revisions", where: "article_id IN (SELECT id FROM articles WHERE workspace_id=$1)"},
	{name: "article_tags", where: "article_id IN (SELECT id FROM articles WHERE workspace_id=$1)"},
	{name: "article_relations", where: "article_id IN (SELECT id FROM articles WHERE workspace_id=$1)"},
	{name: "message_attachments", where: "message_id IN (SELECT m.id FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE c.workspace_id=$1)"},
}

// nullableMemberFields are foreign keys to workspace_members that may be
// cleared when an archive is restored into a workspace with a different
// membership roster. The member itself is intentionally not part of a
// workspace archive because users and credentials belong to the installation,
// not to portable customer data.
var nullableMemberFields = map[string][]string{
	"teams":                    {"lead_id"},
	"companies":                {"owner_id"},
	"customers":                {"owner_id"},
	"conversations":            {"assignee_id"},
	"tickets":                  {"assignee_id"},
	"ticket_links":             {"created_by"},
	"saved_views":              {"owner_id"},
	"macros":                   {"owner_id"},
	"saved_replies":            {"owner_id"},
	"customer_notes":           {"author_id"},
	"identity_merge_history":   {"merged_by", "reversed_by"},
	"blocked_contacts":         {"blocked_by"},
	"widget_config_versions":   {"changed_by"},
	"announcements":            {"created_by"},
	"feedback_items":           {"created_by_member_id"},
	"feedback_status_history":  {"actor_id"},
	"articles":                 {"author_id"},
	"article_revisions":        {"edited_by"},
	"changelog_entries":        {"created_by"},
	"survey_responses":         {"agent_id"},
	"automation_rules":         {"created_by"},
	"automation_rule_versions": {"changed_by"},
	"scheduled_actions":        {"created_by"},
	"api_keys":                 {"created_by"},
	"webhook_endpoints":        {"created_by"},
	"integration_connections":  {"created_by"},
	"saved_reports":            {"owner_id"},
	"tasks":                    {"assignee_id", "created_by"},
}

// requiredMemberRows are member-owned relation rows that cannot be restored
// without a matching target member. Skipping them is deterministic and safe;
// the main support records remain importable and the operator can recreate
// followers/preferences after inviting the target members.
var requiredMemberRows = map[string]string{
	"conversation_followers":   "member_id",
	"ticket_followers":         "member_id",
	"composer_drafts":          "member_id",
	"notification_preferences": "member_id",
}

func Export(ctx context.Context, pool *database.Pool, workspaceID string, now time.Time) (*Archive, []TableSummary, error) {
	if workspaceID == "" {
		return nil, nil, errors.New("portability: workspace id is required")
	}
	archive := &Archive{Version: CurrentVersion, SourceWorkspaceID: workspaceID, ExportedAt: now, Tables: make(map[string][]json.RawMessage)}
	var workspace json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object('id',id,'name',name,'slug',slug,'settings',settings) FROM workspaces WHERE id=$1`, workspaceID).Scan(&workspace); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, errors.New("portability: workspace not found")
		}
		return nil, nil, err
	}
	if err := json.Unmarshal(workspace, &archive.Workspace); err != nil {
		return nil, nil, err
	}
	summaries := make([]TableSummary, 0, len(tableSpecs))
	for _, spec := range tableSpecs {
		rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT to_jsonb(t) FROM %s t WHERE %s`, spec.name, spec.where), workspaceID)
		if err != nil {
			return nil, nil, fmt.Errorf("portability: export %s: %w", spec.name, err)
		}
		items := make([]json.RawMessage, 0)
		for rows.Next() {
			var item json.RawMessage
			if err := rows.Scan(&item); err != nil {
				rows.Close()
				return nil, nil, err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, err
		}
		rows.Close()
		archive.Tables[spec.name] = items
		summaries = append(summaries, TableSummary{Name: spec.name, Rows: len(items)})
	}
	return archive, summaries, nil
}

func Import(ctx context.Context, pool *database.Pool, archive *Archive, targetWorkspaceID string, dryRun bool) ([]TableSummary, error) {
	if archive == nil || archive.Version != CurrentVersion {
		return nil, fmt.Errorf("portability: unsupported archive version")
	}
	if targetWorkspaceID == "" {
		return nil, errors.New("portability: target workspace id is required")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1)`, targetWorkspaceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("portability: target workspace not found")
	}
	summaries := make([]TableSummary, 0, len(tableSpecs))
	for _, spec := range tableSpecs {
		rows := archive.Tables[spec.name]
		summary := TableSummary{Name: spec.name, Rows: len(rows)}
		if spec.direct {
			ids := archiveIDs(rows)
			if len(ids) > 0 {
				var existing int
				query := fmt.Sprintf(`SELECT count(*) FROM %s WHERE workspace_id=$1 AND id = ANY($2::text[])`, spec.name)
				if err := pool.QueryRow(ctx, query, targetWorkspaceID, ids).Scan(&existing); err != nil {
					return nil, fmt.Errorf("portability: preview %s: %w", spec.name, err)
				}
				summary.Existing = existing
				summary.New = len(ids) - existing
			}
		}
		summaries = append(summaries, summary)
	}
	if dryRun {
		return summaries, nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	for _, spec := range tableSpecs {
		for _, raw := range archive.Tables[spec.name] {
			if spec.direct {
				var object map[string]any
				if err := json.Unmarshal(raw, &object); err != nil {
					return nil, err
				}
				object["workspace_id"] = targetWorkspaceID
				raw, err = json.Marshal(object)
				if err != nil {
					return nil, err
				}
			}
			if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s SELECT * FROM jsonb_populate_record(NULL::%s,$1::jsonb) ON CONFLICT DO NOTHING`, spec.name, spec.name), raw); err != nil {
				return nil, fmt.Errorf("portability: import %s: %w", spec.name, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return summaries, nil
}

// ImportChunk applies one bounded, idempotent batch of an archive. The cursor
// is a table index plus a row offset rather than a database offset: archive
// order is immutable, and a worker can safely retry a committed batch after a
// lease loss because every insert remains ON CONFLICT DO NOTHING.
func ImportChunk(ctx context.Context, pool *database.Pool, archive *Archive, targetWorkspaceID string, tableIndex, rowIndex, limit int) (nextTable, nextRow, processed int, done bool, err error) {
	if err := validateImportTarget(ctx, pool, archive, targetWorkspaceID); err != nil {
		return tableIndex, rowIndex, 0, false, err
	}
	if limit <= 0 {
		limit = 100
	}
	if tableIndex < 0 || rowIndex < 0 || tableIndex > len(tableSpecs) {
		return tableIndex, rowIndex, 0, false, errors.New("portability: invalid import progress cursor")
	}

	for tableIndex < len(tableSpecs) {
		spec := tableSpecs[tableIndex]
		rows := archive.Tables[spec.name]
		if rowIndex >= len(rows) {
			tableIndex++
			rowIndex = 0
			continue
		}
		end := rowIndex + limit
		if end > len(rows) {
			end = len(rows)
		}
		batchSize := end - rowIndex

		tx, err := pool.Begin(ctx)
		if err != nil {
			return tableIndex, rowIndex, 0, false, err
		}
		for _, raw := range rows[rowIndex:end] {
			if err := insertArchiveRow(ctx, tx, spec, raw, targetWorkspaceID); err != nil {
				_ = tx.Rollback(ctx)
				return tableIndex, rowIndex, 0, false, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return tableIndex, rowIndex, 0, false, err
		}

		rowIndex = end
		if rowIndex >= len(rows) {
			tableIndex++
			rowIndex = 0
			for tableIndex < len(tableSpecs) && len(archive.Tables[tableSpecs[tableIndex].name]) == 0 {
				tableIndex++
			}
		}
		return tableIndex, rowIndex, batchSize, tableIndex >= len(tableSpecs), nil
	}
	return tableIndex, rowIndex, 0, true, nil
}

func validateImportTarget(ctx context.Context, pool *database.Pool, archive *Archive, targetWorkspaceID string) error {
	if archive == nil || archive.Version != CurrentVersion {
		return fmt.Errorf("portability: unsupported archive version")
	}
	if targetWorkspaceID == "" {
		return errors.New("portability: target workspace id is required")
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1)`, targetWorkspaceID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errors.New("portability: target workspace not found")
	}
	return nil
}

func insertArchiveRow(ctx context.Context, tx pgx.Tx, spec struct {
	name   string
	where  string
	direct bool
}, raw json.RawMessage, targetWorkspaceID string) error {
	if spec.direct || len(nullableMemberFields[spec.name]) > 0 || requiredMemberRows[spec.name] != "" {
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		if spec.direct {
			object["workspace_id"] = targetWorkspaceID
		}
		if field := requiredMemberRows[spec.name]; field != "" {
			if rawMember, ok := object[field].(string); ok && rawMember != "" {
				var memberExists bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND id=$2)`, targetWorkspaceID, rawMember).Scan(&memberExists); err != nil {
					return err
				}
				if !memberExists {
					return nil
				}
			}
		}
		for _, field := range nullableMemberFields[spec.name] {
			rawMember, ok := object[field].(string)
			if !ok || rawMember == "" {
				continue
			}
			var memberExists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND id=$2)`, targetWorkspaceID, rawMember).Scan(&memberExists); err != nil {
				return err
			}
			if !memberExists {
				object[field] = nil
			}
		}
		var err error
		raw, err = json.Marshal(object)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s SELECT * FROM jsonb_populate_record(NULL::%s,$1::jsonb) ON CONFLICT DO NOTHING`, spec.name, spec.name), raw); err != nil {
		return fmt.Errorf("portability: import %s: %w", spec.name, err)
	}
	return nil
}

func archiveIDs(rows []json.RawMessage) []string {
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, raw := range rows {
		var object map[string]any
		if json.Unmarshal(raw, &object) != nil {
			continue
		}
		id, ok := object["id"].(string)
		if !ok || id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
