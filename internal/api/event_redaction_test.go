package api

import "testing"

func TestRedactEventPayloadRecursesAndPreservesSafeFields(t *testing.T) {
	input := map[string]any{
		"order_id": "ord_42",
		"email":    "ada@example.com",
		"profile": map[string]any{
			"phone_number": "+250780000000",
			"plan":         "pro",
		},
		"requests": []any{
			map[string]any{"authorization": "Bearer secret", "status": 200.0},
		},
	}

	redacted := redactEventPayload(input)
	if redacted["order_id"] != "ord_42" {
		t.Fatalf("safe field changed: %#v", redacted["order_id"])
	}
	if redacted["email"] != "[REDACTED]" {
		t.Fatalf("email = %#v, want redacted", redacted["email"])
	}
	profile, ok := redacted["profile"].(map[string]any)
	if !ok || profile["phone_number"] != "[REDACTED]" || profile["plan"] != "pro" {
		t.Fatalf("nested safe object = %#v", redacted["profile"])
	}
	requests, ok := redacted["requests"].([]any)
	if !ok {
		t.Fatalf("requests type = %T", redacted["requests"])
	}
	request, ok := requests[0].(map[string]any)
	if !ok || request["authorization"] != "[REDACTED]" || request["status"] != 200.0 {
		t.Fatalf("nested array object = %#v", requests[0])
	}
}

func TestSensitiveEventKeyNormalizesCommonSpellings(t *testing.T) {
	for _, key := range []string{"email", "customer_email", "phone_number", "client-phone", "access-token", "api_secret", "Authorization", "social_security_number"} {
		if !sensitiveEventKey(key) {
			t.Fatalf("sensitiveEventKey(%q) = false", key)
		}
	}
	for _, key := range []string{"order_id", "plan", "status", "page_url"} {
		if sensitiveEventKey(key) {
			t.Fatalf("sensitiveEventKey(%q) = true", key)
		}
	}
}
