package file

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrTooLarge          = errors.New("file: upload exceeds the configured size limit")
	ErrSizeMismatch      = errors.New("file: upload size does not match its declaration")
	ErrMimeNotAllowed    = errors.New("file: MIME type is not allowed")
	ErrUnsafePath        = errors.New("file: unsafe storage path")
	ErrInvalidOwner      = errors.New("file: owner is not valid for this workspace")
	ErrInvalidAttachment = errors.New("file: attachment is not valid for this workspace")
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

// Store is the byte-storage boundary. Metadata and authorization remain in
// PostgreSQL; implementations only know how to commit, open, and delete an
// object addressed by workspace and file id.
type Store interface {
	Save(context.Context, Upload) (StoredObject, error)
	Open(context.Context, string, string) (io.ReadCloser, error)
	Delete(context.Context, string, string) error
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

// S3Store is a small AWS Signature V4 client for S3-compatible services. It
// deliberately uses only the standard library so MinIO, R2, and object-store
// gateways do not add a heavyweight SDK to the single binary.
type S3Store struct {
	endpoint     *url.URL
	region       string
	bucket       string
	accessKey    string
	secretKey    string
	pathStyle    bool
	maxBytes     int64
	allowedMIMEs map[string]struct{}
	client       *http.Client
}

func NewS3Store(endpoint, region, bucket, accessKey, secretKey string, pathStyle bool, maxBytes int64, allowedMIMEs []string) (*S3Store, error) {
	if strings.TrimSpace(bucket) == "" || strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		return nil, errors.New("file: S3 bucket and credentials are required")
	}
	if maxBytes <= 0 {
		return nil, errors.New("file: maximum size must be positive")
	}
	if strings.TrimSpace(region) == "" {
		region = "us-east-1"
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, errors.New("file: S3 endpoint must be an absolute HTTP URL")
	}
	allowed := make(map[string]struct{}, len(allowedMIMEs))
	for _, mime := range allowedMIMEs {
		if mime = strings.ToLower(strings.TrimSpace(mime)); mime != "" {
			allowed[mime] = struct{}{}
		}
	}
	return &S3Store{endpoint: parsed, region: region, bucket: bucket, accessKey: accessKey, secretKey: secretKey, pathStyle: pathStyle, maxBytes: maxBytes, allowedMIMEs: allowed, client: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (s *S3Store) Save(ctx context.Context, upload Upload) (StoredObject, error) {
	if err := validateUpload(upload, s.maxBytes, s.allowedMIMEs); err != nil {
		return StoredObject{}, err
	}
	data, err := io.ReadAll(io.LimitReader(upload.Body, s.maxBytes+1))
	if err != nil {
		return StoredObject{}, fmt.Errorf("file: read S3 upload: %w", err)
	}
	if int64(len(data)) > s.maxBytes {
		return StoredObject{}, ErrTooLarge
	}
	if int64(len(data)) != upload.SizeBytes {
		return StoredObject{}, ErrSizeMismatch
	}
	hash := sha256.Sum256(data)
	key := upload.WorkspaceID + "/" + upload.FileID
	request, err := s.request(ctx, http.MethodPut, key, data, upload.MIMEType, hash)
	if err != nil {
		return StoredObject{}, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return StoredObject{}, fmt.Errorf("file: S3 upload: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return StoredObject{}, fmt.Errorf("file: S3 upload returned %s", response.Status)
	}
	return StoredObject{StorageKey: key, SizeBytes: int64(len(data)), Checksum: hash}, nil
}

func (s *S3Store) Open(ctx context.Context, workspaceID, fileID string) (io.ReadCloser, error) {
	if err := safeSegment(workspaceID); err != nil {
		return nil, err
	}
	if err := safeSegment(fileID); err != nil {
		return nil, err
	}
	request, err := s.request(ctx, http.MethodGet, workspaceID+"/"+fileID, nil, "", sha256.Sum256(nil))
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("file: S3 open: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf("file: S3 open returned %s", response.Status)
	}
	return response.Body, nil
}

func (s *S3Store) Delete(ctx context.Context, workspaceID, fileID string) error {
	if err := safeSegment(workspaceID); err != nil {
		return err
	}
	if err := safeSegment(fileID); err != nil {
		return err
	}
	request, err := s.request(ctx, http.MethodDelete, workspaceID+"/"+fileID, nil, "", sha256.Sum256(nil))
	if err != nil {
		return err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("file: S3 delete: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound && (response.StatusCode < 200 || response.StatusCode >= 300) {
		return fmt.Errorf("file: S3 delete returned %s", response.Status)
	}
	return nil
}

func (s *S3Store) request(ctx context.Context, method, key string, body []byte, mimeType string, payloadHash [sha256.Size]byte) (*http.Request, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		return nil, ErrUnsafePath
	}
	for _, part := range parts {
		if err := safeSegment(part); err != nil {
			return nil, err
		}
	}
	objectURL := *s.endpoint
	if s.pathStyle {
		objectURL.Path = strings.TrimSuffix(objectURL.Path, "/") + "/" + s.bucket + "/" + key
	} else {
		objectURL.Host = s.bucket + "." + objectURL.Host
		objectURL.Path = strings.TrimSuffix(objectURL.Path, "/") + "/" + key
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, objectURL.String(), reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", mimeType)
	}
	request.Host = objectURL.Host
	payload := hex.EncodeToString(payloadHash[:])
	request.Header.Set("x-amz-content-sha256", payload)
	request.Header.Set("x-amz-date", time.Now().UTC().Format("20060102T150405Z"))
	return s.sign(request, payload), nil
}

func (s *S3Store) sign(request *http.Request, payloadHash string) *http.Request {
	date := request.Header.Get("x-amz-date")
	shortDate := date[:8]
	canonicalURI := request.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalHeaders := "host:" + request.Host + "\n" + "x-amz-content-sha256:" + request.Header.Get("x-amz-content-sha256") + "\n" + "x-amz-date:" + date + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	if value := request.Header.Get("Content-Type"); value != "" {
		canonicalHeaders = "content-type:" + strings.TrimSpace(value) + "\n" + canonicalHeaders
		signedHeaders = "content-type;" + signedHeaders
	}
	canonicalRequest := strings.Join([]string{request.Method, canonicalURI, request.URL.RawQuery, canonicalHeaders, signedHeaders, payloadHash}, "\n")
	hash := sha256.Sum256([]byte(canonicalRequest))
	dateScope := shortDate + "/" + s.region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + date + "\n" + dateScope + "\n" + hex.EncodeToString(hash[:])
	kDate := hmacSHA256([]byte("AWS4"+s.secretKey), []byte(shortDate))
	kRegion := hmacSHA256(kDate, []byte(s.region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.accessKey+"/"+dateScope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return request
}

func hmacSHA256(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func validateUpload(upload Upload, maxBytes int64, allowedMIMEs map[string]struct{}) error {
	if err := safeSegment(upload.WorkspaceID); err != nil {
		return fmt.Errorf("workspace id: %w", err)
	}
	if err := safeSegment(upload.FileID); err != nil {
		return fmt.Errorf("file id: %w", err)
	}
	if upload.Body == nil {
		return errors.New("file: upload body is required")
	}
	if upload.SizeBytes < 0 || upload.SizeBytes > maxBytes {
		return ErrTooLarge
	}
	if len(allowedMIMEs) > 0 {
		if _, ok := allowedMIMEs[strings.ToLower(strings.TrimSpace(upload.MIMEType))]; !ok {
			return ErrMimeNotAllowed
		}
	}
	return nil
}

func safeSegment(value string) error {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return ErrUnsafePath
	}
	return nil
}
