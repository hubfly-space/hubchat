package portability

import "testing"

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
