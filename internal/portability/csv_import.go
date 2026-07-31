package portability

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/customer"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/ticket"
)

const maxCSVImportBytes = 100 << 20

type csvImportRow struct {
	line   int
	values map[string]string
}

func normalizeImportKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", KindWorkspace:
		return KindWorkspace
	case "customer", "customers", KindCustomersCSV:
		return KindCustomersCSV
	case "company", "companies", KindCompaniesCSV:
		return KindCompaniesCSV
	case "ticket", "tickets", KindTicketsCSV:
		return KindTicketsCSV
	case "knowledgebase", "knowledge_base", "markdown", KindKnowledgeBaseMarkdown:
		return KindKnowledgeBaseMarkdown
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func validImportKind(kind string) bool {
	switch normalizeImportKind(kind) {
	case KindWorkspace, KindCustomersCSV, KindCompaniesCSV, KindTicketsCSV, KindKnowledgeBaseMarkdown:
		return true
	default:
		return false
	}
}

func validCSVFile(record *filemodule.Record) bool {
	if record == nil || record.SizeBytes <= 0 || record.SizeBytes > maxCSVImportBytes {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(record.Name))
	mime := strings.ToLower(strings.TrimSpace(record.MIMEType))
	return strings.HasSuffix(name, ".csv") || mime == "text/csv" || mime == "application/csv" || mime == "text/plain"
}

func (s *Service) readCSVFile(ctx context.Context, workspaceID string, fileID *string, kind string) ([]csvImportRow, error) {
	if fileID == nil || s.files == nil {
		return nil, errors.New("CSV import file is missing")
	}
	record, opened, err := s.files.Open(ctx, workspaceID, *fileID)
	if err != nil {
		return nil, fmt.Errorf("open CSV: %w", err)
	}
	defer opened.Close()
	if !validCSVFile(record) {
		return nil, errors.New("CSV import file is not valid")
	}
	body, err := io.ReadAll(io.LimitReader(opened, maxCSVImportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read CSV: %w", err)
	}
	if len(body) > maxCSVImportBytes {
		return nil, errors.New("CSV import exceeds the 100 MiB limit")
	}
	return parseCSVImport(body, normalizeImportKind(kind))
}

func parseCSVImport(body []byte, kind string) ([]csvImportRow, error) {
	reader := csv.NewReader(bytes.NewReader(body))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("CSV header is required")
	}
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}
	columns := make([]string, len(header))
	seen := make(map[string]struct{}, len(header))
	for index, raw := range header {
		column := normalizeCSVColumn(raw)
		if column == "" {
			return nil, fmt.Errorf("CSV header column %d is empty", index+1)
		}
		if _, exists := seen[column]; exists {
			return nil, fmt.Errorf("CSV header contains duplicate column %q", column)
		}
		seen[column] = struct{}{}
		columns[index] = column
	}
	if err := validateCSVColumns(kind, seen); err != nil {
		return nil, err
	}

	rows := make([]csvImportRow, 0)
	line := 1
	for {
		record, readErr := reader.Read()
		line++
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read CSV row %d: %w", line, readErr)
		}
		values := make(map[string]string, len(columns))
		nonEmpty := false
		for index, column := range columns {
			value := ""
			if index < len(record) {
				value = strings.TrimSpace(record[index])
			}
			values[column] = value
			nonEmpty = nonEmpty || value != ""
		}
		if !nonEmpty {
			continue
		}
		rows = append(rows, csvImportRow{line: line, values: values})
	}
	return rows, nil
}

func normalizeCSVColumn(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(" ", "_", "-", "_", ".", "_").Replace(value)
	switch value {
	case "externalid":
		return "external_id"
	case "e_mail", "email_address":
		return "email"
	default:
		return value
	}
}

func validateCSVColumns(kind string, columns map[string]struct{}) error {
	if kind == KindCustomersCSV {
		if !hasColumn(columns, "name") && !hasColumn(columns, "email") && !hasColumn(columns, "external_id") {
			return errors.New("customer CSV requires name, email, or external_id")
		}
		return nil
	}
	if kind == KindCompaniesCSV {
		if !hasColumn(columns, "name") {
			return errors.New("company CSV requires a name column")
		}
		return nil
	}
	if kind == KindTicketsCSV {
		if !hasColumn(columns, "title") {
			return errors.New("ticket CSV requires a title column")
		}
		if !hasColumn(columns, "inbox_id") {
			return errors.New("ticket CSV requires an inbox_id column")
		}
		return nil
	}
	return fmt.Errorf("unsupported CSV import kind %q", kind)
}

func hasColumn(columns map[string]struct{}, name string) bool {
	_, ok := columns[name]
	return ok
}

func (s *Service) previewCSVImport(ctx context.Context, workspaceID string, request *Request) ([]TableSummary, error) {
	if request.Kind == KindTicketsCSV && s.tickets == nil {
		return nil, errors.New("portability: ticket import service is unavailable")
	}
	if request.Kind != KindTicketsCSV && s.customers == nil {
		return nil, errors.New("portability: customer import service is unavailable")
	}
	rows, err := s.readCSVFile(ctx, workspaceID, request.FileID, request.Kind)
	if err != nil {
		return nil, err
	}
	existing := 0
	for _, row := range rows {
		externalID := strings.TrimSpace(row.values["external_id"])
		if request.Kind == KindTicketsCSV {
			if externalID == "" {
				continue
			}
			if _, findErr := s.tickets.FindByImportKey(ctx, workspaceID, externalID); findErr == nil {
				existing++
			} else if !errors.Is(findErr, ticket.ErrNotFound) {
				return nil, findErr
			}
			continue
		}
		if externalID == "" {
			continue
		}
		if request.Kind == KindCustomersCSV {
			if _, findErr := s.customers.FindByExternalID(ctx, workspaceID, externalID); findErr == nil {
				existing++
			} else if !errors.Is(findErr, customer.ErrNotFound) {
				return nil, findErr
			}
		} else if _, findErr := s.customers.FindCompanyByExternalID(ctx, workspaceID, externalID); findErr == nil {
			existing++
		} else if !errors.Is(findErr, customer.ErrCompanyNotFound) {
			return nil, findErr
		}
	}
	return []TableSummary{{Name: request.Kind, Rows: len(rows), Existing: existing, New: len(rows) - existing}}, nil
}

func (s *Service) runCSVImport(ctx context.Context, id string, request *Request) error {
	if request.Kind == KindTicketsCSV && s.tickets == nil {
		return s.failImport(ctx, id, errors.New("portability: ticket import service is unavailable"))
	}
	if request.Kind != KindTicketsCSV && s.customers == nil {
		return s.failImport(ctx, id, errors.New("portability: customer import service is unavailable"))
	}
	rows, err := s.readCSVFile(ctx, request.WorkspaceID, request.FileID, request.Kind)
	if err != nil {
		return s.failImport(ctx, id, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE import_requests SET total_rows=$2 WHERE id=$1`, id, len(rows)); err != nil {
		return err
	}
	_, rowIndex, err := s.importCursor(ctx, id)
	if err != nil {
		return err
	}
	if rowIndex < 0 || rowIndex > len(rows) {
		return s.failImport(ctx, id, errors.New("portability: invalid CSV import cursor"))
	}
	actorID := ""
	if request.RequestedBy != nil {
		actorID = *request.RequestedBy
	}
	for rowIndex < len(rows) {
		end := rowIndex + importBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		for _, row := range rows[rowIndex:end] {
			if err := s.importCSVRow(ctx, request.WorkspaceID, actorID, request.Kind, id, row); err != nil {
				return s.failImport(ctx, id, fmt.Errorf("CSV row %d: %w", row.line, err))
			}
		}
		rowIndex = end
		if _, err := s.pool.Exec(ctx, `UPDATE import_request_progress SET table_index=0,row_index=$2,updated_at=now() WHERE import_id=$1`, id, rowIndex); err != nil {
			return err
		}
		if _, err := s.pool.Exec(ctx, `UPDATE import_requests SET processed_rows=$2 WHERE id=$1`, id, rowIndex); err != nil {
			return err
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE import_request_progress SET table_index=1,row_index=0,updated_at=now() WHERE import_id=$1`, id)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE import_requests SET state='completed',processed_rows=$2,failed_rows=0,completed_at=now(),errors='[]'::jsonb WHERE id=$1`, id, len(rows))
	return err
}

func (s *Service) importCSVRow(ctx context.Context, workspaceID, actorID, kind, requestID string, row csvImportRow) error {
	externalID := strings.TrimSpace(row.values["external_id"])
	if externalID == "" {
		externalID = "hubchat:" + requestID + ":" + strconv.Itoa(row.line)
	}
	if kind == KindCustomersCSV {
		return s.importCustomerRow(ctx, workspaceID, actorID, externalID, row.values)
	}
	if kind == KindCompaniesCSV {
		return s.importCompanyRow(ctx, workspaceID, actorID, externalID, row.values)
	}
	return s.importTicketRow(ctx, workspaceID, actorID, externalID, row.values)
}

func (s *Service) importTicketRow(ctx context.Context, workspaceID, actorID, importKey string, values map[string]string) error {
	priority := strings.TrimSpace(values["priority"])
	status := strings.TrimSpace(values["status"])
	channel := strings.TrimSpace(values["channel"])
	if priority == "" {
		priority = "normal"
	}
	if channel == "" {
		channel = "manual"
	}
	dueAt, err := parseCSVTime(values["due_at"])
	if err != nil {
		return err
	}
	request := ticket.CreateRequest{
		Title: values["title"], Description: values["description"],
		Priority: priority, InboxID: strings.TrimSpace(values["inbox_id"]), Channel: channel,
		CustomerID: optionalCSVString(values["customer_id"]), CompanyID: optionalCSVString(values["company_id"]),
		Type: optionalCSVString(values["type"]), AssigneeID: optionalCSVString(values["assignee_id"]),
		TeamID: optionalCSVString(values["team_id"]), DueAt: dueAt, ImportKey: &importKey,
	}
	created, err := s.tickets.Create(ctx, workspaceID, actorID, request)
	if err != nil {
		return err
	}
	if _, err := s.tickets.SetPriority(ctx, workspaceID, actorID, created.ID, priority); err != nil {
		return err
	}
	if request.AssigneeID != nil {
		if _, err := s.tickets.SetAssignee(ctx, workspaceID, actorID, created.ID, request.AssigneeID); err != nil {
			return err
		}
	}
	if request.TeamID != nil {
		if _, err := s.tickets.SetTeam(ctx, workspaceID, actorID, created.ID, request.TeamID); err != nil {
			return err
		}
	}
	if status != "" && status != "new" {
		if _, err := s.tickets.SetStatus(ctx, workspaceID, actorID, created.ID, status); err != nil {
			return err
		}
	}
	return nil
}

func parseCSVTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05Z07:00", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			parsed = parsed.UTC()
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("invalid due_at %q: expected RFC3339 or YYYY-MM-DD", value)
}

func (s *Service) importCustomerRow(ctx context.Context, workspaceID, actorID, externalID string, values map[string]string) error {
	external := externalID
	name := optionalCSVString(values["name"])
	email := optionalCSVString(values["email"])
	customerRecord, err := s.customers.Identify(ctx, workspaceID, nil, name, email, &external, false)
	if err != nil {
		return err
	}
	phone := optionalCSVString(values["phone"])
	language := optionalCSVString(values["language"])
	timezone := optionalCSVString(values["timezone"])
	if phone == nil && language == nil && timezone == nil {
		return nil
	}
	if phone == nil {
		phone = customerRecord.Phone
	}
	if language == nil {
		language = customerRecord.Language
	}
	if timezone == nil {
		timezone = customerRecord.Timezone
	}
	_, err = s.customers.Update(ctx, workspaceID, actorID, customerRecord.ID, customerRecord.Version, customerRecord.Name, customerRecord.Email, phone, language, timezone)
	return err
}

func (s *Service) importCompanyRow(ctx context.Context, workspaceID, actorID, externalID string, values map[string]string) error {
	name := strings.TrimSpace(values["name"])
	domain := optionalCSVString(values["domain"])
	tier := optionalCSVString(values["tier"])
	external := externalID
	companyRecord, err := s.customers.FindCompanyByExternalID(ctx, workspaceID, externalID)
	if errors.Is(err, customer.ErrCompanyNotFound) {
		_, err = s.customers.CreateCompany(ctx, workspaceID, actorID, name, domain, &external, tier)
		return err
	}
	if err != nil {
		return err
	}
	if name == "" {
		name = companyRecord.Name
	}
	if domain == nil {
		domain = companyRecord.Domain
	}
	if tier == nil {
		tier = companyRecord.Tier
	}
	_, err = s.customers.UpdateCompany(ctx, workspaceID, actorID, companyRecord.ID, name, domain, &external, tier, companyRecord.OwnerID)
	return err
}

func optionalCSVString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
