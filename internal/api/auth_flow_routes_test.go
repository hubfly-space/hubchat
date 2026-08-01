package api

import (
	"net/url"
	"testing"
)

func TestAuthMagicLinkPreservesSafeRedirect(t *testing.T) {
	publicURL, err := url.Parse("https://support.example.test/base")
	if err != nil {
		t.Fatal(err)
	}

	got, err := url.Parse(authMagicLink(Deps{PublicURL: publicURL}, "token-value", "/inbox/cnv_123?view=mine"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/base/app/magic-link" {
		t.Fatalf("path = %q, want /base/app/magic-link", got.Path)
	}
	if got.Query().Get("token") != "token-value" {
		t.Fatalf("token = %q, want token-value", got.Query().Get("token"))
	}
	if got.Query().Get("next") != "/inbox/cnv_123?view=mine" {
		t.Fatalf("next = %q, want original path and query", got.Query().Get("next"))
	}
}

func TestAuthMagicLinkWithoutPublicURLIsRelative(t *testing.T) {
	got := authMagicLink(Deps{}, "token-value", "")
	if got != "/app/magic-link?token=token-value" {
		t.Fatalf("link = %q, want relative token link", got)
	}
}

func TestSafeNextRejectsExternalAndAmbiguousTargets(t *testing.T) {
	for _, value := range []string{"https://evil.example", "//evil.example/path", "/\\evil.example", ""} {
		if got := safeNext(value); got != "" {
			t.Fatalf("safeNext(%q) = %q, want empty", value, got)
		}
	}
	if got := safeNext("/inbox?view=mine"); got != "/inbox?view=mine" {
		t.Fatalf("safeNext() = %q, want preserved local path", got)
	}
}
