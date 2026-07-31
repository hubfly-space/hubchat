package file

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreSaveOpenDelete(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 1024, []string{"text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	stored, err := store.Save(context.Background(), Upload{
		WorkspaceID: "wrk_1",
		FileID:      "fil_1",
		Name:        "../../secret.txt",
		MIMEType:    "text/plain",
		SizeBytes:   int64(len("hello")),
		Body:        strings.NewReader("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.StorageKey != "wrk_1/fil_1" {
		t.Fatalf("storage key = %q", stored.StorageKey)
	}

	opened, err := store.Open(context.Background(), "wrk_1", "fil_1")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(opened)
	_ = opened.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q", body)
	}

	if err := store.Delete(context.Background(), "wrk_1", "fil_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.root, "wrk_1", "fil_1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted object still exists: %v", err)
	}
}

func TestLocalStoreRejectsUnsafeAndInvalidUploads(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), 4, []string{"text/plain"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		upload Upload
		want   error
	}{
		{name: "path", upload: Upload{WorkspaceID: "../workspace", FileID: "fil_1", SizeBytes: 0, Body: strings.NewReader("")}, want: ErrUnsafePath},
		{name: "mime", upload: Upload{WorkspaceID: "wrk_1", FileID: "fil_1", MIMEType: "application/octet-stream", SizeBytes: 0, Body: strings.NewReader("")}, want: ErrMimeNotAllowed},
		{name: "too large", upload: Upload{WorkspaceID: "wrk_1", FileID: "fil_1", MIMEType: "text/plain", SizeBytes: 5, Body: strings.NewReader("12345")}, want: ErrTooLarge},
		{name: "size mismatch", upload: Upload{WorkspaceID: "wrk_1", FileID: "fil_1", MIMEType: "text/plain", SizeBytes: 3, Body: strings.NewReader("12")}, want: ErrSizeMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.Save(context.Background(), test.upload)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
