package file

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrTooLarge       = errors.New("file: upload exceeds the configured size limit")
	ErrSizeMismatch   = errors.New("file: upload size does not match its declaration")
	ErrMimeNotAllowed = errors.New("file: MIME type is not allowed")
	ErrUnsafePath     = errors.New("file: unsafe storage path")
)

// Upload is the validated metadata and body for one object. File names are
// display metadata only; the local backend never uses them to construct a
// storage path.
type Upload struct {
	WorkspaceID string
	FileID      string
	Name        string
	MIMEType    string
	SizeBytes   int64
	Body        io.Reader
}

// StoredObject is returned after the bytes have been durably committed.
type StoredObject struct {
	StorageKey string
	SizeBytes  int64
	Checksum   [sha256.Size]byte
}

// LocalStore stores objects below a configured data directory. The workspace
// and file ids are opaque, validated path segments, so a database leak cannot
// turn a user-controlled name into a path traversal primitive.
type LocalStore struct {
	root         string
	maxBytes     int64
	allowedMIMEs map[string]struct{}
}

func NewLocalStore(root string, maxBytes int64, allowedMIMEs []string) (*LocalStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("file: storage root is required")
	}
	if maxBytes <= 0 {
		return nil, errors.New("file: maximum size must be positive")
	}

	allowed := make(map[string]struct{}, len(allowedMIMEs))
	for _, mime := range allowedMIMEs {
		mime = strings.ToLower(strings.TrimSpace(mime))
		if mime != "" {
			allowed[mime] = struct{}{}
		}
	}
	return &LocalStore{root: filepath.Clean(root), maxBytes: maxBytes, allowedMIMEs: allowed}, nil
}

func (s *LocalStore) Save(ctx context.Context, upload Upload) (StoredObject, error) {
	if err := ctx.Err(); err != nil {
		return StoredObject{}, err
	}
	if err := safeSegment(upload.WorkspaceID); err != nil {
		return StoredObject{}, fmt.Errorf("workspace id: %w", err)
	}
	if err := safeSegment(upload.FileID); err != nil {
		return StoredObject{}, fmt.Errorf("file id: %w", err)
	}
	if upload.Body == nil {
		return StoredObject{}, errors.New("file: upload body is required")
	}
	if upload.SizeBytes < 0 || upload.SizeBytes > s.maxBytes {
		return StoredObject{}, ErrTooLarge
	}
	if len(s.allowedMIMEs) > 0 {
		if _, ok := s.allowedMIMEs[strings.ToLower(strings.TrimSpace(upload.MIMEType))]; !ok {
			return StoredObject{}, ErrMimeNotAllowed
		}
	}

	directory := filepath.Join(s.root, upload.WorkspaceID)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return StoredObject{}, fmt.Errorf("file: create workspace directory: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".upload-")
	if err != nil {
		return StoredObject{}, fmt.Errorf("file: create temporary upload: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	hasher := sha256.New()
	written, err := io.Copy(temporary, io.LimitReader(io.TeeReader(upload.Body, hasher), s.maxBytes+1))
	if err != nil {
		_ = temporary.Close()
		return StoredObject{}, fmt.Errorf("file: write upload: %w", err)
	}
	if written > s.maxBytes {
		_ = temporary.Close()
		return StoredObject{}, ErrTooLarge
	}
	if upload.SizeBytes != written {
		_ = temporary.Close()
		return StoredObject{}, ErrSizeMismatch
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return StoredObject{}, fmt.Errorf("file: sync upload: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return StoredObject{}, fmt.Errorf("file: close upload: %w", err)
	}

	key := filepath.ToSlash(filepath.Join(upload.WorkspaceID, upload.FileID))
	target := filepath.Join(s.root, filepath.FromSlash(key))
	if err := os.Rename(temporaryName, target); err != nil {
		return StoredObject{}, fmt.Errorf("file: commit upload: %w", err)
	}

	var checksum [sha256.Size]byte
	copy(checksum[:], hasher.Sum(nil))
	return StoredObject{StorageKey: key, SizeBytes: written, Checksum: checksum}, nil
}

func (s *LocalStore) Open(ctx context.Context, workspaceID, fileID string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(workspaceID, fileID)
	if err != nil {
		return nil, err
	}
	opened, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("file: open object: %w", err)
	}
	return opened, nil
}

func (s *LocalStore) Delete(ctx context.Context, workspaceID, fileID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(workspaceID, fileID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("file: delete object: %w", err)
	}
	return nil
}

func (s *LocalStore) path(workspaceID, fileID string) (string, error) {
	if err := safeSegment(workspaceID); err != nil {
		return "", fmt.Errorf("workspace id: %w", err)
	}
	if err := safeSegment(fileID); err != nil {
		return "", fmt.Errorf("file id: %w", err)
	}
	return filepath.Join(s.root, workspaceID, fileID), nil
}

func safeSegment(value string) error {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return ErrUnsafePath
	}
	return nil
}
