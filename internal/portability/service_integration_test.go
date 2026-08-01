//go:build integration

package portability

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
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

func TestCSVExportJobIsWorkspaceScopedAndProducesManifest(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_csv_export_a','CSV export A','csv-export-a'),
			('wrk_csv_export_b','CSV export B','csv-export-b');
		INSERT INTO customers (id,workspace_id,external_id,email,name,verification)
		VALUES
			('cus_csv_export_a','wrk_csv_export_a','customer-a','a@example.com','Customer A','verified'),
			('cus_csv_export_b','wrk_csv_export_b','customer-b','b@example.com','Customer B','verified')
	`); err != nil {
		t.Fatal(err)
	}
	store, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"text/csv"})
	if err != nil {
		t.Fatal(err)
	}
	files := filemodule.New(pool, store)
	if _, err := pool.Exec(ctx, `INSERT INTO export_requests (id,workspace_id,kind,format,state) VALUES ('exp_csv_export','wrk_csv_export_a',$1,'csv','pending')`, KindCustomersCSV); err != nil {
		t.Fatal(err)
	}
	service := New(pool, files, nil)
	if err := service.RunExport(ctx, "exp_csv_export"); err != nil {
		t.Fatal(err)
	}
	request, err := service.Get(ctx, "wrk_csv_export_a", "exp_csv_export")
	if err != nil {
		t.Fatal(err)
	}
	if request.State != "completed" || request.FileID == nil || request.RowCount == nil || *request.RowCount != 1 {
		t.Fatalf("export request = %+v", request)
	}
	record, err := files.Get(ctx, "wrk_csv_export_a", *request.FileID)
	if err != nil {
		t.Fatal(err)
	}
	_, opened, err := files.Open(ctx, "wrk_csv_export_a", record.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(opened)
	_ = opened.Close()
	if err != nil {
		t.Fatal(err)
	}
	content := string(body)
	if !strings.Contains(content, "external_id,email,name") || !strings.Contains(content, "customer-a") || strings.Contains(content, "customer-b") {
		t.Fatalf("unexpected CSV export: %q", content)
	}
	manifest, err := service.ExportManifest(ctx, "wrk_csv_export_a", request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FileName == "" || manifest.Checksum == "" || manifest.RowCount != 1 || manifest.AttachmentCount != 0 || len(manifest.Tables) != 1 || manifest.Tables[0].Name != "customers" {
		t.Fatalf("CSV manifest = %+v", manifest)
	}
}

func TestExportIncludesCurrentTenantOperationalTables(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_manifest_tables','Manifest tables','manifest-tables')`); err != nil {
		t.Fatal(err)
	}
	archive, _, err := Export(ctx, pool, "wrk_manifest_tables", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"teams", "team_routing_cursors", "customer_notification_preferences",
		"email_delivery_events", "email_suppressions",
	} {
		if _, ok := archive.Tables[name]; !ok {
			t.Fatalf("archive is missing current tenant table %q", name)
		}
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

func TestImportTeamClearsForeignLeadButPreservesRoutingConfig(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_team_source','Team source','team-source'),
			('wrk_team_target','Team target','team-target')
	`); err != nil {
		t.Fatal(err)
	}
	teamIndex := -1
	for index, spec := range tableSpecs {
		if spec.name == "teams" {
			teamIndex = index
			break
		}
	}
	if teamIndex < 0 {
		t.Fatal("teams archive table is missing")
	}
	archive := &Archive{
		Version:           CurrentVersion,
		SourceWorkspaceID: "wrk_team_source",
		ExportedAt:        time.Now().UTC(),
		Tables: map[string][]json.RawMessage{
			"teams": {json.RawMessage(`{"id":"team_imported","workspace_id":"wrk_team_source","name":"Support","description":"","lead_id":"member_from_source","routing_strategy":"round_robin","routing_config":{"languages":["fr"]},"created_at":"2026-07-31T00:00:00Z"}`)},
		},
	}
	nextTable, nextRow, processed, done, err := ImportChunk(ctx, pool, archive, "wrk_team_target", teamIndex, 0, 1)
	if err != nil || !done || processed != 1 || nextTable != len(tableSpecs) || nextRow != 0 {
		t.Fatalf("team import cursor = table %d row %d processed %d done %t err %v", nextTable, nextRow, processed, done, err)
	}
	var leadID *string
	var routingConfig map[string]any
	if err := pool.QueryRow(ctx, `SELECT lead_id,routing_config FROM teams WHERE workspace_id='wrk_team_target' AND id='team_imported'`).Scan(&leadID, &routingConfig); err != nil {
		t.Fatal(err)
	}
	if leadID != nil || routingConfig["languages"] == nil {
		t.Fatalf("imported team lead=%v routing=%v", leadID, routingConfig)
	}
}

func TestImportSkipsPreferencesForMissingTargetMember(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_pref_target','Preference target','pref-target')`); err != nil {
		t.Fatal(err)
	}
	preferenceIndex := -1
	for index, spec := range tableSpecs {
		if spec.name == "notification_preferences" {
			preferenceIndex = index
			break
		}
	}
	if preferenceIndex < 0 {
		t.Fatal("notification preferences archive table is missing")
	}
	archive := &Archive{
		Version:           CurrentVersion,
		SourceWorkspaceID: "wrk_pref_source",
		ExportedAt:        time.Now().UTC(),
		Tables: map[string][]json.RawMessage{
			"notification_preferences": {json.RawMessage(`{"workspace_id":"wrk_pref_source","member_id":"member_from_source","type":"reply","in_app":true,"email":false,"browser":false,"sound":false}`)},
		},
	}
	nextTable, nextRow, processed, done, err := ImportChunk(ctx, pool, archive, "wrk_pref_target", preferenceIndex, 0, 1)
	if err != nil || !done || processed != 1 || nextTable != len(tableSpecs) || nextRow != 0 {
		t.Fatalf("preference import cursor = table %d row %d processed %d done %t err %v", nextTable, nextRow, processed, done, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_preferences WHERE workspace_id='wrk_pref_target'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("imported %d preference rows for a missing target member", count)
	}
}

func TestSweepExpiredExportsRemovesArchiveAndRetainsAuditRow(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ('wrk_expired_export','Expired export','expired-export')`); err != nil {
		t.Fatal(err)
	}
	store, err := filemodule.NewLocalStore(t.TempDir(), 1<<20, []string{"application/gzip"})
	if err != nil {
		t.Fatal(err)
	}
	files := filemodule.New(pool, store)
	record, err := files.Create(ctx, "wrk_expired_export", filemodule.UploadInput{
		Name: "expired.json.gz", MIMEType: "application/gzip", SizeBytes: 3,
		Body: bytes.NewReader([]byte("zip")), OwnerType: "workspace", OwnerID: "wrk_expired_export", UploadedByType: "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO export_requests (id,workspace_id,kind,format,state,file_id,expires_at) VALUES ('exp_expired','wrk_expired_export','workspace','json','completed',$1,now()-interval '1 minute')`, record.ID); err != nil {
		t.Fatal(err)
	}

	service := New(pool, files, nil)
	removed, err := service.SweepExpiredExports(ctx, time.Now().UTC(), 10)
	if err != nil || removed != 1 {
		t.Fatalf("sweep removed=%d err=%v, want one removal", removed, err)
	}
	var state string
	var fileID *string
	if err := pool.QueryRow(ctx, `SELECT state,file_id FROM export_requests WHERE id='exp_expired'`).Scan(&state, &fileID); err != nil {
		t.Fatal(err)
	}
	if state != "expired" || fileID != nil {
		t.Fatalf("expired request state=%q file_id=%v", state, fileID)
	}
	if _, err := files.Get(ctx, "wrk_expired_export", record.ID); err == nil {
		t.Fatal("expired archive metadata still exists")
	}
}
