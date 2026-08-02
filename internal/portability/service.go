package portability

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/feedback"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/hubchat/hubchat/internal/knowledgebase"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/jackc/pgx/v5"
)

const (
	JobExport                 = "portability.export"
	JobImport                 = "portability.import"
	JobExpireExports          = "portability.expire_exports"
	importBatchSize           = 100
	maxArchiveBytes           = MaxArchiveBytes
	KindWorkspace             = "workspace"
	KindCustomersCSV          = "customers_csv"
	KindCompaniesCSV          = "companies_csv"
	KindTicketsCSV            = "tickets_csv"
	KindFeedbackCSV           = "feedback_csv"
	KindAuditCSV              = "audit_csv"
	KindSurveyCSV             = "survey_csv"
	KindKnowledgeBaseMarkdown = "knowledgebase_markdown"
)

type Request struct {
	ID            string         `json:"id"`
	WorkspaceID   string         `json:"workspace_id"`
	Kind          string         `json:"kind"`
	Scope         map[string]any `json:"scope,omitempty"`
	Format        string         `json:"format,omitempty"`
	FileID        *string        `json:"file_id,omitempty"`
	State         string         `json:"state"`
	RowCount      *int64         `json:"row_count,omitempty"`
	TotalRows     *int           `json:"total_rows,omitempty"`
	ProcessedRows int            `json:"processed_rows"`
	FailedRows    int            `json:"failed_rows"`
	Errors        []any          `json:"errors,omitempty"`
	Error         string         `json:"error,omitempty"`
	RequestedBy   *string        `json:"requested_by,omitempty"`
	ExpiresAt     *time.Time     `json:"expires_at,omitempty"`
	CompletedAt   *time.Time     `json:"completed_at,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Manifest describes the immutable result of a completed export. It gives an
// operator enough information to verify a download before moving it between
// installations without exposing storage keys or bypassing file auth.
type Manifest struct {
	ExportID        string                    `json:"export_id"`
	WorkspaceID     string                    `json:"workspace_id"`
	FileID          string                    `json:"file_id"`
	FileName        string                    `json:"file_name"`
	SizeBytes       int64                     `json:"size_bytes"`
	Checksum        string                    `json:"checksum"`
	ExpiresAt       *time.Time                `json:"expires_at,omitempty"`
	RowCount        int64                     `json:"row_count"`
	AttachmentCount int                       `json:"attachment_count"`
	AttachmentBytes int64                     `json:"attachment_bytes"`
	Attachments     []AttachmentManifestEntry `json:"attachments,omitempty"`
	Tables          []TableSummary            `json:"tables"`
}

// AttachmentManifestEntry records the portable identity and checksum of a
// customer attachment without exposing the backend storage key. Binary
// objects are restored separately from the row archive and can be verified by
// this manifest before an installation is put back into service.
type AttachmentManifestEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Checksum  string `json:"checksum"`
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
}

type exportPayload struct {
	RequestID string `json:"request_id"`
}

type importPayload struct {
	RequestID string `json:"request_id"`
}

type Service struct {
	pool          *database.Pool
	files         *filemodule.Service
	jobs          *jobs.Client
	customers     *customer.Service
	tickets       *ticket.Service
	feedback      *feedback.Service
	knowledgebase *knowledgebase.Service
}

func New(pool *database.Pool, files *filemodule.Service, queue *jobs.Client) *Service {
	return &Service{pool: pool, files: files, jobs: queue}
}

// SetCustomerImporter attaches the domain service used by CSV imports. It is
// optional so archive-only callers and CLI tests can keep the smaller wiring.
func (s *Service) SetCustomerImporter(importer *customer.Service) {
	s.customers = importer
}

// SetTicketImporter attaches the domain service used by ticket CSV imports.
func (s *Service) SetTicketImporter(importer *ticket.Service) {
	s.tickets = importer
}

// SetFeedbackImporter attaches the domain service used by feedback CSV jobs.
func (s *Service) SetFeedbackImporter(importer *feedback.Service) {
	s.feedback = importer
}

// SetKnowledgeBaseImporter attaches the service used by Markdown article
// imports. Articles are upserted by workspace, knowledge base, language, and
// slug so a resumed job cannot create a duplicate.
func (s *Service) SetKnowledgeBaseImporter(importer *knowledgebase.Service) {
	s.knowledgebase = importer
}

func (s *Service) CreateExport(ctx context.Context, workspaceID, memberID, kind string, scope map[string]any) (*Request, error) {
	kind = normalizeExportKind(kind)
	if err := validateExportKind(kind); err != nil {
		return nil, err
	}
	if scope == nil {
		scope = map[string]any{}
	}
	scopeJSON, err := json.Marshal(scope)
	if err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixExport)
	var request Request
	format := "json"
	if kind != KindWorkspace {
		format = "csv"
	}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO export_requests(id,workspace_id,kind,scope,format,requested_by)
		VALUES($1,$2,$3,$4::jsonb,$5,NULLIF($6,''))
		RETURNING id,workspace_id,kind,scope,format,file_id,state,row_count,coalesce(error,''),requested_by,expires_at,completed_at,created_at`,
		id, workspaceID, kind, scopeJSON, format, memberID,
	).Scan(exportArgs(&request)...)
	if err != nil {
		return nil, fmt.Errorf("portability: create export: %w", err)
	}
	if s.jobs == nil {
		_ = s.failExport(ctx, request.ID, errors.New("portability: job queue is unavailable"))
		return nil, errors.New("portability: job queue is unavailable")
	}
	if _, err := s.jobs.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Queue: "exports", Type: JobExport, Payload: exportPayload{RequestID: id}, DedupeKey: "portability-export:" + id}); err != nil {
		_ = s.failExport(ctx, id, err)
		return nil, err
	}
	return s.Get(ctx, workspaceID, id)
}

// PreviewExport reads the workspace archive inputs without creating a job or
// file. Callers can use the result to show the operator what will be included
// before the irreversible download is generated.
func (s *Service) PreviewExport(ctx context.Context, workspaceID, kind string, scope map[string]any) ([]TableSummary, error) {
	kind = normalizeExportKind(kind)
	if err := validateExportKind(kind); err != nil {
		return nil, err
	}
	if kind != KindWorkspace {
		return s.previewCSVExport(ctx, workspaceID, kind)
	}
	_, summaries, err := Export(ctx, s.pool, workspaceID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return summaries, nil
}

func validateExportKind(kind string) error {
	if kind == "" || kind == KindWorkspace {
		return nil
	}
	if _, ok := csvExportSpecFor(kind); ok {
		return nil
	}
	return fmt.Errorf("portability: unsupported export kind %q", kind)
}

func normalizeExportKind(kind string) string {
	if strings.TrimSpace(kind) == "" {
		return KindWorkspace
	}
	return strings.TrimSpace(kind)
}

func (s *Service) CreateImport(ctx context.Context, workspaceID, memberID, fileID, kind string, mapping map[string]any, start bool) (*Request, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, errors.New("portability: import file is required")
	}
	kind = normalizeImportKind(kind)
	if !validImportKind(kind) {
		return nil, fmt.Errorf("portability: unsupported import kind %q", kind)
	}
	if mapping == nil {
		mapping = map[string]any{}
	}
	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return nil, err
	}
	if s.files == nil {
		return nil, errors.New("portability: file service is unavailable")
	}
	fileRecord, err := s.files.Get(ctx, workspaceID, fileID)
	if err != nil {
		return nil, fmt.Errorf("portability: import file: %w", err)
	}
	if kind == KindWorkspace {
		if !validArchiveFile(fileRecord) {
			return nil, errors.New("portability: import file must be a JSON gzip archive")
		}
		if _, err := s.readArchive(ctx, workspaceID, &fileRecord.ID); err != nil {
			return nil, fmt.Errorf("portability: validate archive: %w", err)
		}
	} else if kind == KindKnowledgeBaseMarkdown {
		if !validMarkdownFile(fileRecord) {
			return nil, errors.New("portability: Markdown import file must be a Markdown document")
		}
		if _, err := s.readMarkdownFile(ctx, workspaceID, &fileRecord.ID); err != nil {
			return nil, fmt.Errorf("portability: validate Markdown: %w", err)
		}
	} else {
		if !validCSVFile(fileRecord) {
			return nil, errors.New("portability: CSV import file must be a CSV document")
		}
		if _, err := s.readCSVFile(ctx, workspaceID, &fileRecord.ID, kind); err != nil {
			return nil, fmt.Errorf("portability: validate CSV: %w", err)
		}
	}
	if _, ok := mapping["backup_verified"]; !ok {
		mapping["backup_verified"] = false
	}
	if _, ok := mapping["previewed"]; !ok {
		mapping["previewed"] = false
	}
	mappingJSON, err = json.Marshal(mapping)
	if err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixImport)
	if _, err := s.pool.Exec(ctx, `INSERT INTO import_requests(id,workspace_id,kind,file_id,mapping,requested_by) VALUES($1,$2,$3,$4,$5::jsonb,NULLIF($6,''))`, id, workspaceID, kind, fileID, mappingJSON, memberID); err != nil {
		return nil, fmt.Errorf("portability: create import: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO import_request_progress(import_id) VALUES($1)`, id); err != nil {
		return nil, fmt.Errorf("portability: create import progress: %w", err)
	}
	if start {
		return s.ConfirmImport(ctx, workspaceID, id, true)
	}
	return s.GetImport(ctx, workspaceID, id)
}

// ConfirmImport is the explicit safety boundary for an additive archive
// restore. The operator must have reviewed the dry-run and verified a backup
// before the durable import job can be enqueued.
func (s *Service) ConfirmImport(ctx context.Context, workspaceID, id string, backupVerified bool) (*Request, error) {
	if !backupVerified {
		return nil, errors.New("portability: backup verification is required before import")
	}
	request, err := s.GetImport(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if request.State != "pending" {
		return nil, fmt.Errorf("portability: import is already %s", request.State)
	}
	if _, err := s.PreviewImport(ctx, workspaceID, id); err != nil {
		return nil, fmt.Errorf("portability: import preview is required: %w", err)
	}
	if s.jobs == nil {
		return nil, errors.New("portability: job queue is unavailable")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE import_requests SET mapping = mapping || '{"backup_verified":true}'::jsonb WHERE workspace_id=$1 AND id=$2 AND state='pending'`, workspaceID, id); err != nil {
		return nil, err
	}
	if _, err := s.jobs.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Queue: "imports", Type: JobImport, Payload: importPayload{RequestID: id}, DedupeKey: "portability-import:" + id}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
		return nil, err
	}
	return s.GetImport(ctx, workspaceID, id)
}

func (s *Service) List(ctx context.Context, workspaceID, state string, limit int) ([]Request, error) {
	return s.ListPage(ctx, workspaceID, state, time.Time{}, "", limit)
}

// ListPage returns export requests in newest-first order. The timestamp and
// id cursor makes the history stable while new jobs are being created.
func (s *Service) ListPage(ctx context.Context, workspaceID, state string, before time.Time, beforeID string, limit int) ([]Request, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT id,workspace_id,kind,scope,format,file_id,state,row_count,coalesce(error,''),requested_by,expires_at,completed_at,created_at FROM export_requests WHERE workspace_id=$1`
	args := []any{workspaceID}
	if state != "" {
		query += ` AND state=$2`
		args = append(args, state)
	}
	if !before.IsZero() {
		query += fmt.Sprintf(` AND (created_at,id) < ($%d,$%d)`, len(args)+1, len(args)+2)
		args = append(args, before, beforeID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Request, 0)
	for rows.Next() {
		var item Request
		if err := rows.Scan(exportArgs(&item)...); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListImports(ctx context.Context, workspaceID, state string, limit int) ([]Request, error) {
	return s.ListImportsPage(ctx, workspaceID, state, time.Time{}, "", limit)
}

// ListImportsPage is the cursor-paginated import counterpart to ListPage.
func (s *Service) ListImportsPage(ctx context.Context, workspaceID, state string, before time.Time, beforeID string, limit int) ([]Request, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := `SELECT id,workspace_id,kind,file_id,state,total_rows,processed_rows,failed_rows,errors,requested_by,completed_at,created_at FROM import_requests WHERE workspace_id=$1`
	args := []any{workspaceID}
	if state != "" {
		query += ` AND state=$2`
		args = append(args, state)
	}
	if !before.IsZero() {
		query += fmt.Sprintf(` AND (created_at,id) < ($%d,$%d)`, len(args)+1, len(args)+2)
		args = append(args, before, beforeID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT $` + fmt.Sprint(len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Request, 0)
	for rows.Next() {
		item, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Request, error) {
	var request Request
	err := s.pool.QueryRow(ctx, `SELECT id,workspace_id,kind,scope,format,file_id,state,row_count,coalesce(error,''),requested_by,expires_at,completed_at,created_at FROM export_requests WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(exportArgs(&request)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("portability: export request not found")
	}
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func (s *Service) GetImport(ctx context.Context, workspaceID, id string) (*Request, error) {
	var request Request
	err := s.pool.QueryRow(ctx, `SELECT id,workspace_id,kind,file_id,state,total_rows,processed_rows,failed_rows,errors,requested_by,completed_at,created_at FROM import_requests WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(importArgs(&request)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("portability: import request not found")
	}
	if err != nil {
		return nil, err
	}
	return &request, nil
}

func (s *Service) PreviewImport(ctx context.Context, workspaceID, id string) ([]TableSummary, error) {
	request, err := s.GetImport(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	var summaries []TableSummary
	if request.Kind == KindWorkspace {
		archive, readErr := s.readArchive(ctx, workspaceID, request.FileID)
		if readErr != nil {
			return nil, readErr
		}
		summaries, err = Import(ctx, s.pool, archive, workspaceID, true)
	} else if request.Kind == KindKnowledgeBaseMarkdown {
		summaries, err = s.previewMarkdownImport(ctx, workspaceID, request)
	} else {
		summaries, err = s.previewCSVImport(ctx, workspaceID, request)
	}
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE import_requests SET mapping = mapping || '{"previewed":true}'::jsonb WHERE workspace_id=$1 AND id=$2 AND state='pending'`, workspaceID, id); err != nil {
		return nil, err
	}
	return summaries, nil
}

// ExportManifest reads only the completed, workspace-owned archive metadata
// and its versioned table envelope. The returned checksum is the checksum of
// the exact compressed file bytes stored by the configured file backend.
func (s *Service) ExportManifest(ctx context.Context, workspaceID, id string) (*Manifest, error) {
	request, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if request.State != "completed" || request.FileID == nil {
		return nil, errors.New("portability: export is not ready for a manifest")
	}
	if s.files == nil {
		return nil, errors.New("portability: file service is unavailable")
	}
	fileRecord, err := s.files.Get(ctx, workspaceID, *request.FileID)
	if err != nil {
		return nil, fmt.Errorf("portability: export file: %w", err)
	}
	var summaries []TableSummary
	var rowCount int64
	if request.Kind != KindWorkspace {
		spec, ok := csvExportSpecFor(request.Kind)
		if !ok {
			return nil, fmt.Errorf("portability: unsupported export kind %q", request.Kind)
		}
		rowCount = valueOrZero(request.RowCount)
		summaries = []TableSummary{{Name: spec.Name, Rows: int(rowCount)}}
	} else {
		archive, readErr := s.readArchive(ctx, workspaceID, request.FileID)
		if readErr != nil {
			return nil, readErr
		}
		summaries = make([]TableSummary, 0, len(tableSpecs))
		for _, spec := range tableSpecs {
			rows := len(archive.Tables[spec.name])
			summaries = append(summaries, TableSummary{Name: spec.name, Rows: rows})
			rowCount += int64(rows)
		}
	}
	manifest := &Manifest{
		ExportID: request.ID, WorkspaceID: request.WorkspaceID, FileID: fileRecord.ID,
		FileName: fileRecord.Name, SizeBytes: fileRecord.SizeBytes,
		Checksum: filemodule.ChecksumHex(fileRecord.Checksum), ExpiresAt: request.ExpiresAt,
		RowCount: rowCount, Tables: summaries,
	}
	if request.Kind == KindWorkspace {
		archive, readErr := s.readArchive(ctx, workspaceID, request.FileID)
		if readErr != nil {
			return nil, readErr
		}
		manifest.Attachments = make([]AttachmentManifestEntry, 0)
		for _, raw := range archive.Tables["files"] {
			var object struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				MIMEType  string `json:"mime_type"`
				SizeBytes int64  `json:"size_bytes"`
				Checksum  string `json:"checksum"`
				OwnerType string `json:"owner_type"`
				OwnerID   string `json:"owner_id"`
			}
			if err := json.Unmarshal(raw, &object); err != nil {
				return nil, fmt.Errorf("portability: manifest file row: %w", err)
			}
			if object.OwnerType != "workspace" {
				manifest.AttachmentCount++
				manifest.AttachmentBytes += object.SizeBytes
				manifest.Attachments = append(manifest.Attachments, AttachmentManifestEntry{
					ID: object.ID, Name: object.Name, MIMEType: object.MIMEType, SizeBytes: object.SizeBytes,
					Checksum: strings.TrimPrefix(object.Checksum, `\x`), OwnerType: object.OwnerType, OwnerID: object.OwnerID,
				})
			}
		}
	}
	return manifest, nil
}

// SweepExpiredExports removes archive bytes after their download window while
// retaining the request row as an audit record. The sweep is safe to rerun:
// a failed storage deletion leaves the row eligible for the next attempt, and
// a successful deletion makes the foreign-key file_id nullable before the row
// is marked expired.
func (s *Service) SweepExpiredExports(ctx context.Context, now time.Time, limit int) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, file_id
		FROM export_requests
		WHERE state IN ('completed','expired')
		  AND expires_at IS NOT NULL AND expires_at <= $1
		ORDER BY expires_at ASC, id ASC
		LIMIT $2
	`, now, limit)
	if err != nil {
		return 0, fmt.Errorf("portability: list expired exports: %w", err)
	}
	type candidate struct {
		id, workspaceID string
		fileID          *string
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.id, &item.workspaceID, &item.fileID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("portability: scan expired export: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	removed := 0
	var firstErr error
	for _, item := range candidates {
		if item.fileID != nil && strings.TrimSpace(*item.fileID) != "" {
			if s.files == nil {
				if firstErr == nil {
					firstErr = errors.New("portability: file service is unavailable")
				}
				continue
			}
			if err := s.files.Delete(ctx, item.workspaceID, *item.fileID); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("portability: delete expired export %s: %w", item.id, err)
				}
				continue
			}
		}
		result, err := s.pool.Exec(ctx, `
			UPDATE export_requests
			SET state='expired'
			WHERE id=$1 AND workspace_id=$2
			  AND state IN ('completed','expired')
			  AND expires_at IS NOT NULL AND expires_at <= $3
		`, item.id, item.workspaceID, now)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("portability: expire export %s: %w", item.id, err)
			}
			continue
		}
		removed += int(result.RowsAffected())
	}
	return removed, firstErr
}

func (s *Service) RunExport(ctx context.Context, id string) error {
	var request Request
	err := s.pool.QueryRow(ctx, `UPDATE export_requests SET state='running' WHERE id=$1 AND state='pending' RETURNING id,workspace_id,kind,scope,format,file_id,state,row_count,coalesce(error,''),requested_by,expires_at,completed_at,created_at`, id).Scan(exportArgs(&request)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var buffer bytes.Buffer
	var summaries []TableSummary
	mimeType := "application/gzip"
	fileName := "hubchat-" + request.WorkspaceID + "-" + id + ".json.gz"
	if request.Kind == KindWorkspace || request.Kind == "" {
		archive, exportSummaries, exportErr := Export(ctx, s.pool, request.WorkspaceID, time.Now().UTC())
		if exportErr != nil {
			return s.failExport(ctx, id, exportErr)
		}
		summaries = exportSummaries
		gzipWriter := gzip.NewWriter(&buffer)
		if encodeErr := json.NewEncoder(gzipWriter).Encode(archive); encodeErr != nil {
			_ = gzipWriter.Close()
			return s.failExport(ctx, id, encodeErr)
		}
		if closeErr := gzipWriter.Close(); closeErr != nil {
			return s.failExport(ctx, id, closeErr)
		}
	} else {
		var exportErr error
		buffer, summaries, exportErr = s.exportCSV(ctx, request.WorkspaceID, request.Kind)
		if exportErr != nil {
			return s.failExport(ctx, id, exportErr)
		}
		mimeType = "text/csv"
		fileName = "hubchat-" + request.WorkspaceID + "-" + id + "-" + request.Kind + ".csv"
	}
	if int64(buffer.Len()) > maxArchiveBytes {
		return s.failExport(ctx, id, errors.New("portability: export exceeds the 512 MiB limit"))
	}
	if s.files == nil {
		return s.failExport(ctx, id, errors.New("portability: file service is unavailable"))
	}
	fileRecord, err := s.files.Create(ctx, request.WorkspaceID, filemodule.UploadInput{
		Name: fileName, MIMEType: mimeType,
		SizeBytes: int64(buffer.Len()), Body: bytes.NewReader(buffer.Bytes()), OwnerType: "workspace", OwnerID: request.WorkspaceID,
		UploadedByType: "system",
	})
	if err != nil {
		return s.failExport(ctx, id, err)
	}
	var rowCount int64
	for _, summary := range summaries {
		rowCount += int64(summary.Rows)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE export_requests SET state='completed',file_id=$2,row_count=$3,expires_at=now()+interval '7 days',completed_at=now(),error=NULL WHERE id=$1`, id, fileRecord.ID, rowCount); err != nil {
		return s.failExport(ctx, id, err)
	}
	return nil
}

func (s *Service) RunImport(ctx context.Context, id string) error {
	request, err := s.claimImport(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if request.Kind != KindWorkspace {
		if request.Kind == KindKnowledgeBaseMarkdown {
			return s.runMarkdownImport(ctx, id, request)
		}
		return s.runCSVImport(ctx, id, request)
	}
	archive, err := s.readArchive(ctx, request.WorkspaceID, request.FileID)
	if err != nil {
		return s.failImport(ctx, id, err)
	}
	summaries, err := Import(ctx, s.pool, archive, request.WorkspaceID, true)
	if err != nil {
		return s.failImport(ctx, id, err)
	}
	total := 0
	for _, summary := range summaries {
		total += summary.Rows
	}
	if _, err := s.pool.Exec(ctx, `UPDATE import_requests SET total_rows=$2 WHERE id=$1`, id, total); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO import_request_progress(import_id) VALUES($1) ON CONFLICT (import_id) DO NOTHING`, id); err != nil {
		return err
	}
	tableIndex, rowIndex, err := s.importCursor(ctx, id)
	if err != nil {
		return err
	}
	for {
		nextTable, nextRow, _, done, err := ImportChunk(ctx, s.pool, archive, request.WorkspaceID, tableIndex, rowIndex, importBatchSize)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return s.failImport(ctx, id, err)
		}
		processed := importedRowsBefore(archive, nextTable, nextRow)
		if _, err := s.pool.Exec(ctx, `UPDATE import_request_progress SET table_index=$2,row_index=$3,updated_at=now() WHERE import_id=$1`, id, nextTable, nextRow); err != nil {
			return err
		}
		if _, err := s.pool.Exec(ctx, `UPDATE import_requests SET processed_rows=$2 WHERE id=$1`, id, processed); err != nil {
			return err
		}
		if done {
			_, err = s.pool.Exec(ctx, `UPDATE import_requests SET state='completed',processed_rows=$2,failed_rows=0,completed_at=now(),errors='[]'::jsonb WHERE id=$1`, id, processed)
			return err
		}
		tableIndex, rowIndex = nextTable, nextRow
	}
}

func (s *Service) claimImport(ctx context.Context, id string) (*Request, error) {
	var request Request
	err := s.pool.QueryRow(ctx, `UPDATE import_requests SET state='running' WHERE id=$1 AND state IN ('pending','running') RETURNING id,workspace_id,kind,file_id,state,total_rows,processed_rows,failed_rows,errors,requested_by,completed_at,created_at`, id).Scan(importArgs(&request)...)
	return &request, err
}

func (s *Service) readArchive(ctx context.Context, workspaceID string, fileID *string) (*Archive, error) {
	if fileID == nil || s.files == nil {
		return nil, errors.New("portability: archive file is missing")
	}
	record, opened, err := s.files.Open(ctx, workspaceID, *fileID)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	compressed, err := io.ReadAll(io.LimitReader(opened, maxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("portability: read archive: %w", err)
	}
	if int64(len(compressed)) > maxArchiveBytes {
		return nil, errors.New("portability: archive exceeds the 512 MiB compressed limit")
	}
	if err := filemodule.VerifyChecksum(bytes.NewReader(compressed), record.Checksum); err != nil {
		return nil, fmt.Errorf("portability: verify archive checksum: %w", err)
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("portability: open archive: %w", err)
	}
	defer gzipReader.Close()
	var archive Archive
	if err := json.NewDecoder(io.LimitReader(gzipReader, maxArchiveBytes)).Decode(&archive); err != nil {
		return nil, fmt.Errorf("portability: decode archive: %w", err)
	}
	if archive.Version != CurrentVersion || archive.SourceWorkspaceID == "" || archive.Tables == nil {
		return nil, errors.New("portability: unsupported or incomplete archive")
	}
	return &archive, nil
}

func validArchiveFile(record *filemodule.Record) bool {
	if record == nil || record.SizeBytes <= 0 || record.SizeBytes > maxArchiveBytes {
		return false
	}
	name := strings.ToLower(record.Name)
	mime := strings.ToLower(record.MIMEType)
	return strings.HasSuffix(name, ".json.gz") || mime == "application/gzip" || mime == "application/json" || mime == "application/octet-stream"
}

func (s *Service) importCursor(ctx context.Context, id string) (int, int, error) {
	var tableIndex, rowIndex int
	err := s.pool.QueryRow(ctx, `SELECT table_index,row_index FROM import_request_progress WHERE import_id=$1`, id).Scan(&tableIndex, &rowIndex)
	return tableIndex, rowIndex, err
}

func importedRowsBefore(archive *Archive, tableIndex, rowIndex int) int {
	if tableIndex >= len(tableSpecs) {
		tableIndex = len(tableSpecs)
	}
	total := 0
	for index := 0; index < tableIndex; index++ {
		total += len(archive.Tables[tableSpecs[index].name])
	}
	if tableIndex < len(tableSpecs) {
		rows := len(archive.Tables[tableSpecs[tableIndex].name])
		if rowIndex > rows {
			rowIndex = rows
		}
		total += rowIndex
	}
	return total
}

func (s *Service) failExport(ctx context.Context, id string, cause error) error {
	_, updateErr := s.pool.Exec(ctx, `UPDATE export_requests SET state='failed',error=left($2,2000),completed_at=now() WHERE id=$1`, id, cause.Error())
	return errors.Join(cause, updateErr)
}

func (s *Service) failImport(ctx context.Context, id string, cause error) error {
	_, updateErr := s.pool.Exec(ctx, `UPDATE import_requests SET state='failed',failed_rows=failed_rows+1,errors=jsonb_build_array(jsonb_build_object('error',left($2,2000))),completed_at=now() WHERE id=$1`, id, cause.Error())
	return errors.Join(cause, updateErr)
}

func exportArgs(item *Request) []any {
	return []any{&item.ID, &item.WorkspaceID, &item.Kind, &item.Scope, &item.Format, &item.FileID, &item.State, &item.RowCount, &item.Error, &item.RequestedBy, &item.ExpiresAt, &item.CompletedAt, &item.CreatedAt}
}

func importArgs(item *Request) []any {
	return []any{&item.ID, &item.WorkspaceID, &item.Kind, &item.FileID, &item.State, &item.TotalRows, &item.ProcessedRows, &item.FailedRows, &item.Errors, &item.RequestedBy, &item.CompletedAt, &item.CreatedAt}
}

func scanImport(row interface{ Scan(...any) error }) (*Request, error) {
	var item Request
	if err := row.Scan(importArgs(&item)...); err != nil {
		return nil, err
	}
	return &item, nil
}
