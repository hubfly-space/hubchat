package portability

import (
	"strings"
	"testing"
)

func TestValidateExportKind(t *testing.T) {
	for _, kind := range []string{"", KindWorkspace, KindCustomersCSV, KindCompaniesCSV, KindTicketsCSV, KindFeedbackCSV, KindAuditCSV, KindSurveyCSV} {
		if err := validateExportKind(kind); err != nil {
			t.Fatalf("validateExportKind(%q): %v", kind, err)
		}
	}
	if err := validateExportKind("unknown"); err == nil {
		t.Fatal("expected unknown export kind to be rejected")
	}
	if got := normalizeExportKind(""); got != KindWorkspace {
		t.Fatalf("normalizeExportKind empty = %q", got)
	}
}

func TestCSVExportSpecsAreWorkspaceScoped(t *testing.T) {
	for _, kind := range []string{KindCustomersCSV, KindCompaniesCSV, KindTicketsCSV, KindFeedbackCSV, KindAuditCSV, KindSurveyCSV} {
		spec, ok := csvExportSpecFor(kind)
		if !ok || spec.Name == "" || spec.Filename == "" || spec.Query == "" {
			t.Fatalf("missing CSV export spec for %q: %+v", kind, spec)
		}
		if !strings.Contains(spec.Query, "$1") {
			t.Fatalf("CSV export %q is not parameterized by workspace", kind)
		}
	}
}
