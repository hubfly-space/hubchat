package portability

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestArchiveManifestIsUniqueAndTenantScoped(t *testing.T) {
	seen := make(map[string]struct{}, len(tableSpecs))
	for _, spec := range tableSpecs {
		if _, exists := seen[spec.name]; exists {
			t.Fatalf("duplicate archive table %q", spec.name)
		}
		seen[spec.name] = struct{}{}
		if spec.direct && spec.where != "workspace_id=$1" {
			t.Fatalf("direct table %q must use a workspace predicate, got %q", spec.name, spec.where)
		}
	}
	if len(seen) < 70 {
		t.Fatalf("archive manifest unexpectedly small: %d tables", len(seen))
	}
}

func TestImportRejectsUnsupportedArchiveBeforeDatabaseAccess(t *testing.T) {
	_, err := Import(nil, nil, &Archive{Version: CurrentVersion + 1}, "wrk_target", true)
	if err == nil || err.Error() != "portability: unsupported archive version" {
		t.Fatalf("expected unsupported archive error, got %v", err)
	}
}

func validArchiveForTest() *Archive {
	tables := make(map[string][]json.RawMessage, len(tableSpecs))
	for _, spec := range tableSpecs {
		tables[spec.name] = []json.RawMessage{}
	}
	return &Archive{
		Version:           CurrentVersion,
		SourceWorkspaceID: "wrk_verify",
		Workspace:         map[string]any{"id": "wrk_verify", "name": "Verify"},
		ExportedAt:        time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
		Tables:            tables,
	}
}

func TestValidateArchiveRequiresCompleteMatchingManifest(t *testing.T) {
	inspection, err := ValidateArchive(validArchiveForTest())
	if err != nil {
		t.Fatalf("validate archive: %v", err)
	}
	if inspection.RowCount != 0 || len(inspection.Tables) != len(tableSpecs) {
		t.Fatalf("inspection = %+v", inspection)
	}

	missing := validArchiveForTest()
	delete(missing.Tables, tableSpecs[0].name)
	if _, err := ValidateArchive(missing); err == nil || !strings.Contains(err.Error(), "table") {
		t.Fatalf("missing table error = %v", err)
	}

	mismatch := validArchiveForTest()
	mismatch.Workspace["id"] = "wrk_other"
	if _, err := ValidateArchive(mismatch); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("workspace mismatch error = %v", err)
	}
}

func TestValidateArchiveRejectsNonObjectRowsAndCountsAttachments(t *testing.T) {
	archive := validArchiveForTest()
	archive.Tables["files"] = []json.RawMessage{
		json.RawMessage(`{"id":"file_1","owner_type":"ticket","size_bytes":42}`),
	}
	inspection, err := ValidateArchive(archive)
	if err != nil {
		t.Fatalf("validate attachment archive: %v", err)
	}
	if inspection.AttachmentCount != 1 || inspection.AttachmentBytes != 42 || inspection.RowCount != 1 {
		t.Fatalf("attachment inspection = %+v", inspection)
	}

	archive.Tables["files"] = []json.RawMessage{json.RawMessage(`null`)}
	if _, err := ValidateArchive(archive); err == nil || !strings.Contains(err.Error(), "invalid files row") {
		t.Fatalf("null row error = %v", err)
	}
}
