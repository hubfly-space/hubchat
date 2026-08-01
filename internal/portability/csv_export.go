package portability

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"time"
)

// csvExportSpec is intentionally a static allowlist. Export kinds are user
// input, but the SQL must never be assembled from that input.
type csvExportSpec struct {
	Name     string
	Filename string
	Query    string
}

var csvExportSpecs = map[string]csvExportSpec{
	KindCustomersCSV: {
		Name: "customers", Filename: "customers.csv",
		Query: `SELECT external_id,email,name,phone,verification,language,timezone,created_at
			FROM customers WHERE workspace_id=$1 ORDER BY created_at ASC,id ASC`,
	},
	KindCompaniesCSV: {
		Name: "companies", Filename: "companies.csv",
		Query: `SELECT external_id,name,domain,tier,owner_id,created_at,updated_at
			FROM companies WHERE workspace_id=$1 ORDER BY created_at ASC,id ASC`,
	},
	KindTicketsCSV: {
		Name: "tickets", Filename: "tickets.csv",
		Query: `SELECT number,prefix,title,description,status,priority,type,customer_id,company_id,
			inbox_id,channel,assignee_id,team_id,due_at,first_resolved_at,resolved_at,closed_at,
			reopen_count,created_at,updated_at
			FROM tickets WHERE workspace_id=$1 ORDER BY created_at ASC,id ASC`,
	},
	KindFeedbackCSV: {
		Name: "feedback_items", Filename: "feedback.csv",
		Query: `SELECT b.slug AS board_slug,f.title,f.description,f.type,f.status,f.visibility,
			f.submitter_id,f.created_by_member_id,f.company_id,f.product_area,f.priority,
			f.vote_count,f.comment_count,f.subscriber_count,f.merged_into_id,f.created_at,f.updated_at
			FROM feedback_items f JOIN feedback_boards b ON b.id=f.board_id AND b.workspace_id=f.workspace_id
			WHERE f.workspace_id=$1 ORDER BY f.created_at ASC,f.id ASC`,
	},
	KindAuditCSV: {
		Name: "audit_logs", Filename: "audit.csv",
		Query: `SELECT occurred_at,actor_type,actor_id,actor_name,action,entity_type,entity_id,
			request_id,ip,metadata
			FROM audit_logs WHERE workspace_id=$1 ORDER BY occurred_at ASC,id ASC`,
	},
	KindSurveyCSV: {
		Name: "survey_responses", Filename: "surveys.csv",
		Query: `SELECT s.name AS survey_name,s.type AS survey_type,r.id,r.customer_id,r.conversation_id,
			r.ticket_id,r.agent_id,r.team_id,r.score,r.comment,r.sent_at,r.submitted_at
			FROM survey_responses r JOIN surveys s ON s.id=r.survey_id AND s.workspace_id=r.workspace_id
			WHERE r.workspace_id=$1 ORDER BY coalesce(r.submitted_at,r.sent_at) ASC,r.id ASC`,
	},
}

func csvExportSpecFor(kind string) (csvExportSpec, bool) {
	spec, ok := csvExportSpecs[normalizeExportKind(kind)]
	return spec, ok
}

func (s *Service) previewCSVExport(ctx context.Context, workspaceID, kind string) ([]TableSummary, error) {
	spec, ok := csvExportSpecFor(kind)
	if !ok {
		return nil, fmt.Errorf("portability: unsupported export kind %q", kind)
	}
	var count int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM (`+spec.Query+`) AS export_rows`, workspaceID).Scan(&count); err != nil {
		return nil, fmt.Errorf("portability: preview %s: %w", kind, err)
	}
	return []TableSummary{{Name: spec.Name, Rows: int(count)}}, nil
}

func (s *Service) exportCSV(ctx context.Context, workspaceID, kind string) (bytes.Buffer, []TableSummary, error) {
	spec, ok := csvExportSpecFor(kind)
	if !ok {
		return bytes.Buffer{}, nil, fmt.Errorf("portability: unsupported export kind %q", kind)
	}
	rows, err := s.pool.Query(ctx, spec.Query, workspaceID)
	if err != nil {
		return bytes.Buffer{}, nil, fmt.Errorf("portability: query %s export: %w", kind, err)
	}
	defer rows.Close()

	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	header := make([]string, len(rows.FieldDescriptions()))
	for index, field := range rows.FieldDescriptions() {
		header[index] = field.Name
	}
	if err := writer.Write(header); err != nil {
		return bytes.Buffer{}, nil, fmt.Errorf("portability: write %s header: %w", kind, err)
	}
	var count int
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return bytes.Buffer{}, nil, fmt.Errorf("portability: read %s row: %w", kind, err)
		}
		cells := make([]string, len(values))
		for index, value := range values {
			cells[index] = csvCell(value)
		}
		if err := writer.Write(cells); err != nil {
			return bytes.Buffer{}, nil, fmt.Errorf("portability: write %s row: %w", kind, err)
		}
		count++
		if int64(buffer.Len()) > maxArchiveBytes {
			return bytes.Buffer{}, nil, fmt.Errorf("portability: %s export exceeds the 512 MiB limit", kind)
		}
	}
	if err := rows.Err(); err != nil {
		return bytes.Buffer{}, nil, fmt.Errorf("portability: read %s export: %w", kind, err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return bytes.Buffer{}, nil, fmt.Errorf("portability: finalize %s export: %w", kind, err)
	}
	return buffer, []TableSummary{{Name: spec.Name, Rows: count}}, nil
}

func csvCell(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case []byte:
		return string(typed)
	case string:
		return typed
	default:
		if data, err := json.Marshal(value); err == nil && (isStructuredCSVValue(value) || len(data) > 0 && data[0] == '{') {
			return string(data)
		}
		return fmt.Sprint(value)
	}
}

func isStructuredCSVValue(value any) bool {
	switch value.(type) {
	case []string, []int, []int32, []int64, []float32, []float64, map[string]any:
		return true
	default:
		return false
	}
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
