package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

// Service owns file metadata and uses Store for the bytes. The database row is
// the authorization boundary: callers never open a storage key directly.
type Service struct {
	pool    *database.Pool
	store   Store
	backend string
}

func New(pool *database.Pool, store Store) *Service {
	backend := "local"
	if _, ok := store.(*S3Store); ok {
		backend = "s3"
	}
	return &Service{pool: pool, store: store, backend: backend}
}

type Record struct {
	ID             string
	WorkspaceID    string
	StorageKey     string
	Backend        string
	Name           string
	MIMEType       string
	SizeBytes      int64
	Checksum       []byte
	OwnerType      string
	OwnerID        string
	UploadedByType string
	UploadedByID   string
	CommittedAt    time.Time
	CreatedAt      time.Time
}

type UploadInput struct {
	Name           string
	MIMEType       string
	SizeBytes      int64
	Body           io.Reader
	OwnerType      string
	OwnerID        string
	UploadedByType string
	UploadedByID   string
}

func (s *Service) Create(ctx context.Context, workspaceID string, input UploadInput) (*Record, error) {
	if workspaceID == "" {
		return nil, errors.New("file: workspace id is required")
	}
	if err := s.validateOwner(ctx, workspaceID, input.OwnerType, input.OwnerID); err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixFile)
	stored, err := s.store.Save(ctx, Upload{
		WorkspaceID: workspaceID,
		FileID:      id,
		Name:        input.Name,
		MIMEType:    input.MIMEType,
		SizeBytes:   input.SizeBytes,
		Body:        input.Body,
	})
	if err != nil {
		return nil, err
	}

	uploaderType := input.UploadedByType
	if uploaderType == "" {
		uploaderType = "user"
	}
	var record Record
	err = s.pool.QueryRow(ctx, `
		INSERT INTO files (
			id, workspace_id, storage_key, backend, name, mime_type, size_bytes,
			checksum, owner_type, owner_id, uploaded_by_type, uploaded_by_id, committed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''), $11, NULLIF($12, ''), now())
		RETURNING id, workspace_id, storage_key, backend, name, mime_type, size_bytes,
		          coalesce(checksum, ''::bytea), coalesce(owner_type, ''), coalesce(owner_id, ''),
		          uploaded_by_type, coalesce(uploaded_by_id, ''), committed_at, created_at
	`, id, workspaceID, stored.StorageKey, s.backend, input.Name, input.MIMEType, stored.SizeBytes,
		stored.Checksum[:], input.OwnerType, input.OwnerID, uploaderType, input.UploadedByID,
	).Scan(
		&record.ID, &record.WorkspaceID, &record.StorageKey, &record.Backend,
		&record.Name, &record.MIMEType, &record.SizeBytes, &record.Checksum,
		&record.OwnerType, &record.OwnerID, &record.UploadedByType, &record.UploadedByID,
		&record.CommittedAt, &record.CreatedAt,
	)
	if err != nil {
		_ = s.store.Delete(context.Background(), workspaceID, id)
		return nil, fmt.Errorf("file: create metadata: %w", err)
	}
	return &record, nil
}

func (s *Service) validateOwner(ctx context.Context, workspaceID, ownerType, ownerID string) error {
	if ownerType == "" && ownerID == "" {
		return nil
	}
	if ownerType == "" || ownerID == "" {
		return ErrInvalidOwner
	}
	var query string
	switch ownerType {
	case "message":
		query = `SELECT EXISTS (SELECT 1 FROM messages WHERE id = $1 AND workspace_id = $2)`
	case "conversation":
		query = `SELECT EXISTS (SELECT 1 FROM conversations WHERE id = $1 AND workspace_id = $2)`
	case "ticket":
		query = `SELECT EXISTS (SELECT 1 FROM tickets WHERE id = $1 AND workspace_id = $2)`
	case "article":
		query = `SELECT EXISTS (SELECT 1 FROM articles WHERE id = $1 AND workspace_id = $2)`
	case "form_submission":
		query = `SELECT EXISTS (SELECT 1 FROM form_submissions WHERE id = $1 AND workspace_id = $2)`
	case "workspace":
		query = `SELECT EXISTS (SELECT 1 FROM workspaces WHERE id = $1 AND id = $2)`
	default:
		return ErrInvalidOwner
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, query, ownerID, workspaceID).Scan(&exists); err != nil {
		return fmt.Errorf("file: validate owner: %w", err)
	}
	if !exists {
		return ErrInvalidOwner
	}
	return nil
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Record, error) {
	var record Record
	err := s.pool.QueryRow(ctx, `
		SELECT id, workspace_id, storage_key, backend, name, mime_type, size_bytes,
		       coalesce(checksum, ''::bytea), coalesce(owner_type, ''), coalesce(owner_id, ''),
		       uploaded_by_type, coalesce(uploaded_by_id, ''), committed_at, created_at
		FROM files
		WHERE workspace_id = $1 AND id = $2 AND committed_at IS NOT NULL
	`, workspaceID, id).Scan(
		&record.ID, &record.WorkspaceID, &record.StorageKey, &record.Backend,
		&record.Name, &record.MIMEType, &record.SizeBytes, &record.Checksum,
		&record.OwnerType, &record.OwnerID, &record.UploadedByType, &record.UploadedByID,
		&record.CommittedAt, &record.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("file: get %s: %w", id, err)
	}
	return &record, nil
}

// AttachToMessage links already-uploaded files to a message. Uploads are kept
// separate from message creation so a client can stream a file first and then
// submit one idempotent message containing all selected files. Every lookup is
// workspace-scoped and the transaction prevents a partial attachment list.
func (s *Service) AttachToMessage(ctx context.Context, workspaceID, messageID string, fileIDs []string) error {
	if messageID == "" || len(fileIDs) == 0 {
		return nil
	}
	unique := make([]string, 0, len(fileIDs))
	seen := make(map[string]struct{}, len(fileIDs))
	for _, id := range fileIDs {
		if id == "" {
			return ErrInvalidAttachment
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var messageExists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM messages WHERE workspace_id = $1 AND id = $2)
		`, workspaceID, messageID).Scan(&messageExists); err != nil {
			return fmt.Errorf("file: validate message attachment target: %w", err)
		}
		if !messageExists {
			return ErrInvalidAttachment
		}

		var fileCount int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM files
			WHERE workspace_id = $1 AND id = ANY($2::text[]) AND committed_at IS NOT NULL
		`, workspaceID, unique).Scan(&fileCount); err != nil {
			return fmt.Errorf("file: validate message attachments: %w", err)
		}
		if fileCount != len(unique) {
			return ErrInvalidAttachment
		}

		for position, fileID := range unique {
			if _, err := tx.Exec(ctx, `
				INSERT INTO message_attachments (message_id, file_id, position)
				VALUES ($1, $2, $3)
				ON CONFLICT (message_id, file_id) DO UPDATE SET position = EXCLUDED.position
			`, messageID, fileID, position); err != nil {
				return fmt.Errorf("file: attach %s: %w", fileID, err)
			}
		}
		return nil
	})
}

// MessageAttachments returns committed files in the order selected by the
// sender. It is intentionally a second query from message loading so callers
// that do not render attachments do not pay for the join.
func (s *Service) MessageAttachments(ctx context.Context, workspaceID, messageID string) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.id, f.workspace_id, f.storage_key, f.backend, f.name, f.mime_type,
		       f.size_bytes, coalesce(f.checksum, ''::bytea), coalesce(f.owner_type, ''),
		       coalesce(f.owner_id, ''), f.uploaded_by_type, coalesce(f.uploaded_by_id, ''),
		       f.committed_at, f.created_at
		FROM message_attachments ma
		JOIN files f ON f.id = ma.file_id AND f.workspace_id = $1 AND f.committed_at IS NOT NULL
		WHERE ma.message_id = $2
		ORDER BY ma.position ASC, f.created_at ASC, f.id ASC
	`, workspaceID, messageID)
	if err != nil {
		return nil, fmt.Errorf("file: list message attachments: %w", err)
	}
	defer rows.Close()

	attachments := make([]Record, 0)
	for rows.Next() {
		var record Record
		if err := rows.Scan(
			&record.ID, &record.WorkspaceID, &record.StorageKey, &record.Backend,
			&record.Name, &record.MIMEType, &record.SizeBytes, &record.Checksum,
			&record.OwnerType, &record.OwnerID, &record.UploadedByType, &record.UploadedByID,
			&record.CommittedAt, &record.CreatedAt,
		); err != nil {
			return nil, err
		}
		attachments = append(attachments, record)
	}
	return attachments, rows.Err()
}

func (s *Service) Open(ctx context.Context, workspaceID, id string) (*Record, io.ReadCloser, error) {
	record, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, nil, err
	}
	opened, err := s.store.Open(ctx, workspaceID, id)
	if err != nil {
		return nil, nil, fmt.Errorf("file: open %s: %w", id, err)
	}
	return record, opened, nil
}

// Delete removes committed metadata and bytes for a workspace-owned file.
// Callers must already have authorized the record; the workspace predicate is
// still enforced here so cleanup jobs cannot cross tenant boundaries.
func (s *Service) Delete(ctx context.Context, workspaceID, id string) error {
	record, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, workspaceID, id); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM files WHERE workspace_id=$1 AND id=$2`, workspaceID, record.ID); err != nil {
		return fmt.Errorf("file: delete metadata: %w", err)
	}
	return nil
}

func ChecksumHex(checksum []byte) string {
	if len(checksum) == 0 {
		return ""
	}
	return hex.EncodeToString(checksum)
}

func VerifyChecksum(body io.Reader, expected []byte) error {
	if len(expected) == 0 {
		return nil
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, body); err != nil {
		return err
	}
	if !equalBytes(hash.Sum(nil), expected) {
		return errors.New("file: checksum mismatch")
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var mismatch byte
	for i := range a {
		mismatch |= a[i] ^ b[i]
	}
	return mismatch == 0
}
