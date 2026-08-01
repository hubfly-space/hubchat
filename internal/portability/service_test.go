package portability

import "testing"

func TestValidateExportKind(t *testing.T) {
	for _, kind := range []string{"", KindWorkspace} {
		if err := validateExportKind(kind); err != nil {
			t.Fatalf("validateExportKind(%q): %v", kind, err)
		}
	}
	if err := validateExportKind("customers_csv"); err == nil {
		t.Fatal("expected unsupported export kind to be rejected")
	}
}
