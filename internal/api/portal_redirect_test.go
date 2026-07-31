package api

import "testing"

func TestSafePortalNext(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "local path", input: "/tickets/tic_123?tab=conversation", want: "/tickets/tic_123?tab=conversation"},
		{name: "fragment", input: "/tickets/tic_123#reply", want: "/tickets/tic_123#reply"},
		{name: "absolute url", input: "https://evil.example/phish", want: ""},
		{name: "protocol relative", input: "//evil.example/phish", want: ""},
		{name: "backslash", input: `/\\evil.example/phish`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safePortalNext(tt.input); got != tt.want {
				t.Fatalf("safePortalNext(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPortalTicketPriority(t *testing.T) {
	for input, want := range map[string]string{
		"blocking": "urgent", "major": "high", "minor": "normal", "question": "low",
		"urgent": "urgent", "unknown": "normal", "": "normal",
	} {
		if got := portalTicketPriority(input); got != want {
			t.Fatalf("portalTicketPriority(%q) = %q, want %q", input, got, want)
		}
	}
}
