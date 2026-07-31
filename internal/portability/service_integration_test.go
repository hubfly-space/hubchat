//go:build integration

package portability

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/database/dbtest"
	filemodule "github.com/hubchat/hubchat/internal/file"
)

func TestExportManifestIncludesChecksumAndAttachmentTotals(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_manifest_a','Manifest A','manifest-a')`); err != nil {
		t.Fatal(err)
	}

	archive := &Archive{
		Version:           CurrentVersion,
		SourceWorkspaceID: "wrk_manifest_a",
		ExportedAt:        time.Now().UTC(),
		Tables: map[string][]json.RawMessage{
			"files": {json.RawMessage(`{"size_bytes":42,"owner_type":"ticket"}`)},
		},
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if err := json.NewEncoder(writer).Encode(archive); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"application/gzip"})
	if err != nil {
		t.Fatal(err)
	}
	files := filemodule.New(pool, store)
	record, err := files.Create(ctx, "wrk_manifest_a", filemodule.UploadInput{
		Name: "workspace.json.gz", MIMEType: "application/gzip", SizeBytes: int64(compressed.Len()),
		Body: bytes.NewReader(compressed.Bytes()), OwnerType: "workspace", OwnerID: "wrk_manifest_a", UploadedByType: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO export_requests (id,workspace_id,kind,format,state,file_id,row_count,expires_at) VALUES ('exp_manifest','wrk_manifest_a','workspace','json','completed',$1,1,now()+interval '7 days')`, record.ID); err != nil {
		t.Fatal(err)
	}

	manifest, err := New(pool, files, nil).ExportManifest(context.Background(), "wrk_manifest_a", "exp_manifest")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Checksum == "" || manifest.SizeBytes != int64(compressed.Len()) || manifest.RowCount != 1 || manifest.AttachmentCount != 1 || manifest.AttachmentBytes != 42 {
		t.Fatalf("manifest = %+v", manifest)
	}
}
