package widget

import "testing"

func TestLocalizedContentUsesLocaleFallbackWithoutExposingCatalog(t *testing.T) {
	content := map[string]any{
		"title":             "Support",
		"input_placeholder": "Write a message…",
		"translations": map[string]any{
			"fr": map[string]any{"title": "Assistance", "input_placeholder": "Écrivez un message…"},
			"ar": map[string]any{"title": "الدعم"},
		},
	}

	fr := localizedContent(content, "fr-FR")
	if fr["title"] != "Assistance" || fr["input_placeholder"] != "Écrivez un message…" {
		t.Fatalf("fr-FR did not use the fr translation: %#v", fr)
	}
	if _, ok := fr["translations"]; ok {
		t.Fatal("localized content exposed the translation catalog")
	}

	ar := localizedContent(content, "ar")
	if ar["title"] != "الدعم" || ar["input_placeholder"] != "Write a message…" {
		t.Fatalf("ar did not preserve base fallback: %#v", ar)
	}

	ja := localizedContent(content, "ja")
	if ja["title"] != "Support" || ja["input_placeholder"] != "Write a message…" {
		t.Fatalf("unknown locale did not use base content: %#v", ja)
	}
}

func TestNormalizeLanguage(t *testing.T) {
	if got := normalizeLanguage(" FR_fr "); got != "fr-fr" {
		t.Fatalf("normalizeLanguage() = %q, want fr-fr", got)
	}
	if got := normalizeLanguage("this-locale-name-is-too-long-to-accept"); got != "" {
		t.Fatalf("long locale was accepted: %q", got)
	}
}
