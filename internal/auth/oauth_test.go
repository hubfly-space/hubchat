package auth

import (
	"encoding/json"
	"testing"
)

func TestAllowedOAuthDomain(t *testing.T) {
	if !allowedOAuthDomain("person@example.com", nil) {
		t.Fatal("empty allowlist should allow configured deployment identities")
	}
	if !allowedOAuthDomain("person@example.com", []string{"example.com"}) {
		t.Fatal("matching domain was rejected")
	}
	for _, email := range []string{"person@other.example", "not-an-email"} {
		if allowedOAuthDomain(email, []string{"example.com"}) {
			t.Fatalf("%q was allowed by the domain policy", email)
		}
	}
}

func TestOAuthIdentityHelpersRequireVerifiedEmail(t *testing.T) {
	verified := map[string]json.RawMessage{
		"sub":            json.RawMessage(`"subject"`),
		"email":          json.RawMessage(`"person@example.com"`),
		"email_verified": json.RawMessage(`"true"`),
	}
	if firstString(verified, "sub") != "subject" || !firstBool(verified, "email_verified") {
		t.Fatal("failed to decode the provider identity")
	}
}

func TestOAuthIdentityProfiles(t *testing.T) {
	microsoft := parseOAuthIdentity(map[string]json.RawMessage{
		"id":                json.RawMessage(`"directory-id"`),
		"userPrincipalName": json.RawMessage(`"person@example.com"`),
		"displayName":       json.RawMessage(`"Directory Person"`),
	}, "microsoft")
	if microsoft.Subject != "directory-id" || microsoft.Email != "person@example.com" || microsoft.Name != "Directory Person" || !microsoft.Verified {
		t.Fatalf("microsoft identity = %+v", microsoft)
	}

	google := parseOAuthIdentity(map[string]json.RawMessage{
		"sub":            json.RawMessage(`"google-id"`),
		"email":          json.RawMessage(`"person@example.com"`),
		"email_verified": json.RawMessage(`true`),
		"name":           json.RawMessage(`"Google Person"`),
	}, "google")
	if google.Subject != "google-id" || google.Email != "person@example.com" || google.Name != "Google Person" || !google.Verified {
		t.Fatalf("google identity = %+v", google)
	}
}
