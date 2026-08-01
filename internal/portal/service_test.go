package portal

import "testing"

func TestNormalizeDomain(t *testing.T) {
	tests := []struct {
		input string
		want  string
		valid bool
	}{
		{" Support.Example.com. ", "support.example.com", true},
		{"localhost", "", false},
		{"https://support.example.com", "", false},
		{"-support.example.com", "", false},
		{"support..example.com", "", false},
	}
	for _, test := range tests {
		got, err := normalizeDomain(test.input)
		if test.valid {
			if err != nil || got != test.want {
				t.Fatalf("normalizeDomain(%q) = %q, %v; want %q", test.input, got, err, test.want)
			}
		} else if err == nil {
			t.Fatalf("normalizeDomain(%q) accepted invalid hostname %q", test.input, got)
		}
	}
}
