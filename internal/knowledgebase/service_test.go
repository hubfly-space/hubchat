package knowledgebase

import "testing"

func TestSlugPattern(t *testing.T) {
	for _, test := range []struct {
		slug  string
		valid bool
	}{
		{"getting-started", true}, {"v2", true}, {"", false}, {"Needs Spaces", false}, {"-leading", false}, {"trailing-", false},
	} {
		if got := slugPattern.MatchString(test.slug); got != test.valid {
			t.Fatalf("slug %q valid=%v, got %v", test.slug, test.valid, got)
		}
	}
}

func TestUniqueLanguages(t *testing.T) {
	got := uniqueLanguages([]string{"en", " en ", "pt", "", "pt"})
	if len(got) != 2 || got[0] != "en" || got[1] != "pt" {
		t.Fatalf("unexpected languages: %#v", got)
	}
}

func TestValidState(t *testing.T) {
	if !validState("published") || !validState("in_review") || validState("deleted") {
		t.Fatal("unexpected article state validation")
	}
}

func TestValidChangelogKind(t *testing.T) {
	for _, kind := range []string{"added", "improved", "fixed", "removed"} {
		if !validChangelogKind(kind) {
			t.Fatalf("expected changelog kind %q to be valid", kind)
		}
	}
	if validChangelogKind("draft") {
		t.Fatal("draft is a publication state, not a changelog kind")
	}
}
