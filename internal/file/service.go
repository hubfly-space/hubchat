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
)

// Service owns file metadata and uses Store for the bytes. The database row is
// the authorization boundary: callers never open a storage key directly.
type Service struct {
	pool  *database.Pool
	store *LocalStore
}

func New(pool *database.Pool, store *LocalStore) *Service {
	return &Service{pool: pool, store: store}
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
		VALUES ($1, $2, $3, 'local', $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''), $10, NULLIF($11, ''), now())
		RETURNING id, workspace_id, storage_key, backend, name, mime_type, size_bytes,
		          coalesce(checksum, ''::bytea), coalesce(owner_type, ''), coalesce(owner_id, ''),
		          uploaded_by_type, coalesce(uploaded_by_id, ''), committed_at, created_at
	`, id, workspaceID, stored.StorageKey, input.Name, input.MIMEType, stored.SizeBytes,
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
