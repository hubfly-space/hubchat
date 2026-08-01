//go:build provider

package file

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"testing"
	"time"
)

// TestS3ProviderSmoke exercises the real S3-compatible adapter against an
// operator-provided endpoint. It is intentionally behind the provider build
// tag because the normal suite must not require MinIO, credentials, or a
// network service.
func TestS3ProviderSmoke(t *testing.T) {
	if os.Getenv("HUBCHAT_RUN_PROVIDER_TESTS") != "1" {
		t.Skip("set HUBCHAT_RUN_PROVIDER_TESTS=1 to exercise real provider adapters")
	}

	store, err := NewS3Store(
		envOr("HUBCHAT_PROVIDER_S3_ENDPOINT", "http://127.0.0.1:9000"),
		envOr("HUBCHAT_PROVIDER_S3_REGION", "us-east-1"),
		envOr("HUBCHAT_PROVIDER_S3_BUCKET", "hubchat"),
		envOr("HUBCHAT_PROVIDER_S3_ACCESS_KEY", "hubchat"),
		envOr("HUBCHAT_PROVIDER_S3_SECRET_KEY", "hubchat-dev-secret"),
		os.Getenv("HUBCHAT_PROVIDER_S3_PATH_STYLE") != "0",
		1<<20,
		[]string{"text/plain"},
	)
	if err != nil {
		t.Fatalf("create S3 store: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	workspaceID := "provider-smoke"
	fileID := "object-" + time.Now().UTC().Format("20060102-150405.000000000")
	body := []byte("Hubchat provider smoke: ✓")
	stored, err := store.Save(ctx, Upload{
		WorkspaceID: workspaceID,
		FileID:      fileID,
		Name:        "provider-smoke.txt",
		MIMEType:    "text/plain",
		SizeBytes:   int64(len(body)),
		Body:        bytes.NewReader(body),
	})
	if err != nil {
		t.Fatalf("S3 save: %v", err)
	}
	defer func() { _ = store.Delete(context.Background(), workspaceID, fileID) }()

	wantChecksum := sha256.Sum256(body)
	if stored.StorageKey != workspaceID+"/"+fileID || stored.SizeBytes != int64(len(body)) || stored.Checksum != wantChecksum {
		t.Fatalf("stored object = %+v, want key/size/checksum for %d bytes", stored, len(body))
	}

	opened, err := store.Open(ctx, workspaceID, fileID)
	if err != nil {
		t.Fatalf("S3 open: %v", err)
	}
	readBody, readErr := io.ReadAll(opened)
	_ = opened.Close()
	if readErr != nil {
		t.Fatalf("read S3 object: %v", readErr)
	}
	if !bytes.Equal(readBody, body) {
		t.Fatalf("S3 body = %q, want %q", readBody, body)
	}

	if err := store.Delete(ctx, workspaceID, fileID); err != nil {
		t.Fatalf("S3 delete: %v", err)
	}
	if _, err := store.Open(ctx, workspaceID, fileID); err == nil {
		t.Fatal("S3 object is still readable after delete")
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
