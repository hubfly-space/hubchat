package webhook

import "testing"

func TestValidateURL(t *testing.T) {
	for _, test := range []struct {
		name string
		url string
		valid bool
	}{
		{"https", "https://hooks.example.test/events", true},
		{"http", "http://localhost:8080/hook", true},
		{"missing host", "https:///hook", false},
		{"unsupported scheme", "ftp://hooks.example.test/hook", false},
		{"userinfo", "https://user:pass@hooks.example.test/hook", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateURL(test.url)
			if (err == nil) != test.valid { t.Fatalf("validateURL(%q) error=%v, valid=%v", test.url, err, test.valid) }
		})
	}
}

func TestNormalizeEvents(t *testing.T) {
	got, err := normalizeEvents([]string{" message.created ", "message.created", "ticket.created"})
	if err != nil { t.Fatal(err) }
	if len(got) != 2 || got[0] != "message.created" || got[1] != "ticket.created" { t.Fatalf("unexpected events: %#v", got) }
	if _, err := normalizeEvents([]string{"not-an-event"}); err == nil { t.Fatal("expected invalid event error") }
}

func TestSecretHint(t *testing.T) {
	if got := secretHint("whsec_abcdefghijkl"); got != "ijkl" { t.Fatalf("got %q", got) }
}
