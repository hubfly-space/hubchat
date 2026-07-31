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

func TestImportChunkResumesAndRemapsWorkspaceRows(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_import_target','Import target','import-target'),
			('wrk_import_other','Other workspace','import-other');
		INSERT INTO tags (id,workspace_id,name,color) VALUES ('tag_other','wrk_import_other','other',3)
	`); err != nil {
		t.Fatal(err)
	}
	tagIndex := -1
	for index, spec := range tableSpecs {
		if spec.name == "tags" {
			tagIndex = index
			break
		}
	}
	if tagIndex < 0 {
		t.Fatal("tags archive table is missing")
	}
	archive := &Archive{
		Version:           CurrentVersion,
		SourceWorkspaceID: "wrk_import_source",
		ExportedAt:        time.Now().UTC(),
		Tables: map[string][]json.RawMessage{
			"tags": {
				json.RawMessage(`{"id":"tag_one","workspace_id":"wrk_import_source","name":"one","color":3,"created_at":"2026-07-31T00:00:00Z"}`),
				json.RawMessage(`{"id":"tag_two","workspace_id":"wrk_import_source","name":"two","color":4,"created_at":"2026-07-31T00:00:00Z"}`),
			},
		},
	}

	nextTable, nextRow, processed, done, err := ImportChunk(ctx, pool, archive, "wrk_import_target", tagIndex, 0, 1)
	if err != nil || done || nextTable != tagIndex || nextRow != 1 || processed != 1 {
		t.Fatalf("first import chunk = table %d row %d processed %d done %t err %v", nextTable, nextRow, processed, done, err)
	}
	nextTable, nextRow, processed, done, err = ImportChunk(ctx, pool, archive, "wrk_import_target", nextTable, nextRow, 1)
	if err != nil || !done || nextTable != len(tableSpecs) || nextRow != 0 || processed != 1 {
		t.Fatalf("second import chunk = table %d row %d processed %d done %t err %v", nextTable, nextRow, processed, done, err)
	}
	var targetCount, otherCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tags WHERE workspace_id='wrk_import_target'`).Scan(&targetCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tags WHERE workspace_id='wrk_import_other'`).Scan(&otherCount); err != nil {
		t.Fatal(err)
	}
	if targetCount != 2 || otherCount != 1 {
		t.Fatalf("tag counts target=%d other=%d", targetCount, otherCount)
	}
}
