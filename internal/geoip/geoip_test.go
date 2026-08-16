package geoip

import "testing"

func TestMask(t *testing.T) {
	tests := map[string]string{
		"203.0.113.42":          "203.0.113.0/24",
		"2001:db8:abcd:1234::1": "2001:db8:abcd::/48",
		"not-an-ip":             "",
	}
	for input, want := range tests {
		if got := Mask(input); got != want {
			t.Errorf("Mask(%q) = %q, want %q", input, got, want)
		}
	}
}
