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

	"github.com/hubchat/hubchat/internal/database"
	filemodule "github.com/hubchat/hubchat/internal/file"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/jackc/pgx/v5"
)

const (
	JobExport = "portability.export"
	JobImport = "portability.import"
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
	ExportID        string         `json:"export_id"`
	WorkspaceID     string         `json:"workspace_id"`
	FileID          string         `json:"file_id"`
	FileName        string         `json:"file_name"`
	SizeBytes       int64          `json:"size_bytes"`
	Checksum        string         `json:"checksum"`
	ExpiresAt       *time.Time     `json:"expires_at,omitempty"`
	RowCount        int64          `json:"row_count"`
	AttachmentCount int            `json:"attachment_count"`
	AttachmentBytes int64          `json:"attachment_bytes"`
	Tables          []TableSummary `json:"tables"`
}

type exportPayload struct {
	RequestID string `json:"request_id"`
}

type importPayload struct {
	RequestID string `json:"request_id"`
}

type Service struct {
	pool  *database.Pool
	files *filemodule.Service
	jobs  *jobs.Client
}

func New(pool *database.Pool, files *filemodule.Service, queue *jobs.Client) *Service {
	return &Service{pool: pool, files: files, jobs: queue}
}

func (s *Service) CreateExport(ctx context.Context, workspaceID, memberID, kind string, scope map[string]any) (*Request, error) {
	if kind == "" {
		kind = "workspace"
	}
	if kind != "workspace" {
		return nil, errors.New("portability: only workspace archives are currently supported")
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
	err = s.pool.QueryRow(ctx, `
		INSERT INTO export_requests(id,workspace_id,kind,scope,format,requested_by)
		VALUES($1,$2,$3,$4::jsonb,'json',NULLIF($5,''))
		RETURNING id,workspace_id,kind,scope,format,file_id,state,row_count,coalesce(error,''),requested_by,expires_at,completed_at,created_at`,
		id, workspaceID, kind, scopeJSON, memberID,
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

func (s *Service) CreateImport(ctx context.Context, workspaceID, memberID, fileID, kind string, mapping map[string]any) (*Request, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, errors.New("portability: import archive file is required")
	}
	if kind == "" {
		kind = "workspace"
	}
	if kind != "workspace" {
		return nil, errors.New("portability: only workspace archives are currently supported")
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
	if _, err := s.files.Get(ctx, workspaceID, fileID); err != nil {
		return nil, fmt.Errorf("portability: import file: %w", err)
	}
	id := ids.New(ids.PrefixImport)
	if _, err := s.pool.Exec(ctx, `INSERT INTO import_requests(id,workspace_id,kind,file_id,mapping,requested_by) VALUES($1,$2,$3,$4,$5::jsonb,NULLIF($6,''))`, id, workspaceID, kind, fileID, mappingJSON, memberID); err != nil {
		return nil, fmt.Errorf("portability: create import: %w", err)
	}
	if s.jobs == nil {
		_ = s.failImport(ctx, id, errors.New("portability: job queue is unavailable"))
		return nil, errors.New("portability: job queue is unavailable")
	}
	if _, err := s.jobs.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Queue: "imports", Type: JobImport, Payload: importPayload{RequestID: id}, DedupeKey: "portability-import:" + id}); err != nil {
		_ = s.failImport(ctx, id, err)
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
	archive, err := s.readArchive(ctx, workspaceID, request.FileID)
	if err != nil {
		return nil, err
	}
	return Import(ctx, s.pool, archive, workspaceID, true)
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
	archive, err := s.readArchive(ctx, workspaceID, request.FileID)
	if err != nil {
		return nil, err
	}

	summaries := make([]TableSummary, 0, len(tableSpecs))
	var rowCount int64
	for _, spec := range tableSpecs {
		rows := len(archive.Tables[spec.name])
		summaries = append(summaries, TableSummary{Name: spec.name, Rows: rows})
		rowCount += int64(rows)
	}
	manifest := &Manifest{
		ExportID: request.ID, WorkspaceID: request.WorkspaceID, FileID: fileRecord.ID,
		FileName: fileRecord.Name, SizeBytes: fileRecord.SizeBytes,
		Checksum: filemodule.ChecksumHex(fileRecord.Checksum), ExpiresAt: request.ExpiresAt,
		RowCount: rowCount, Tables: summaries,
	}
	for _, raw := range archive.Tables["files"] {
		var object struct {
			SizeBytes int64  `json:"size_bytes"`
			OwnerType string `json:"owner_type"`
		}
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, fmt.Errorf("portability: manifest file row: %w", err)
		}
		if object.OwnerType != "workspace" {
			manifest.AttachmentCount++
			manifest.AttachmentBytes += object.SizeBytes
		}
	}
	return manifest, nil
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
	archive, summaries, err := Export(ctx, s.pool, request.WorkspaceID, time.Now().UTC())
	if err != nil {
		return s.failExport(ctx, id, err)
	}
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	if err := json.NewEncoder(gzipWriter).Encode(archive); err != nil {
		_ = gzipWriter.Close()
		return s.failExport(ctx, id, err)
	}
	if err := gzipWriter.Close(); err != nil {
		return s.failExport(ctx, id, err)
	}
	if s.files == nil {
		return s.failExport(ctx, id, errors.New("portability: file service is unavailable"))
	}
	fileRecord, err := s.files.Create(ctx, request.WorkspaceID, filemodule.UploadInput{
		Name: "hubchat-" + request.WorkspaceID + "-" + id + ".json.gz", MIMEType: "application/gzip",
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
	archive, err := s.readArchive(ctx, request.WorkspaceID, request.FileID)
	if err != nil {
		return s.failImport(ctx, id, err)
	}
	summaries, err := Import(ctx, s.pool, archive, request.WorkspaceID, false)
	if err != nil {
		return s.failImport(ctx, id, err)
	}
	total := 0
	for _, summary := range summaries {
		total += summary.Rows
	}
	_, err = s.pool.Exec(ctx, `UPDATE import_requests SET state='completed',total_rows=$2,processed_rows=$2,failed_rows=0,completed_at=now(),errors='[]'::jsonb WHERE id=$1`, id, total)
	return err
}

func (s *Service) claimImport(ctx context.Context, id string) (*Request, error) {
	var request Request
	err := s.pool.QueryRow(ctx, `UPDATE import_requests SET state='running' WHERE id=$1 AND state='pending' RETURNING id,workspace_id,kind,file_id,state,total_rows,processed_rows,failed_rows,errors,requested_by,completed_at,created_at`, id).Scan(importArgs(&request)...)
	return &request, err
}

func (s *Service) readArchive(ctx context.Context, workspaceID string, fileID *string) (*Archive, error) {
	if fileID == nil || s.files == nil {
		return nil, errors.New("portability: archive file is missing")
	}
	_, opened, err := s.files.Open(ctx, workspaceID, *fileID)
	if err != nil {
		return nil, err
	}
	defer opened.Close()
	gzipReader, err := gzip.NewReader(opened)
	if err != nil {
		return nil, fmt.Errorf("portability: open archive: %w", err)
	}
	defer gzipReader.Close()
	var archive Archive
	if err := json.NewDecoder(io.LimitReader(gzipReader, 512<<20)).Decode(&archive); err != nil {
		return nil, fmt.Errorf("portability: decode archive: %w", err)
	}
	return &archive, nil
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
