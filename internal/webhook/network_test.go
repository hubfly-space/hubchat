package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestWebhookNetworkPolicy(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "::ffff:127.0.0.1", "fe80::1", "fc00::1", "0.0.0.0", "224.0.0.1"} {
		if !restrictedIP(netip.MustParseAddr(raw)) {
			t.Errorf("allowed restricted IP %s", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if restrictedIP(netip.MustParseAddr(raw)) {
			t.Errorf("blocked public IP %s", raw)
		}
	}
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer receiver.Close()
	if _, err := newNetworkPolicy(Options{}).client().Get(receiver.URL); err == nil {
		t.Fatal("default policy reached loopback")
	}
	allowed := newNetworkPolicy(Options{AllowedPrivateHosts: []string{"127.0.0.1"}}).client()
	response, err := allowed.Get(receiver.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatal(response.StatusCode)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, receiver.URL, http.StatusFound) }))
	defer redirect.Close()
	response, err = allowed.Get(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatal("followed webhook redirect")
	}
	if _, err := newNetworkPolicy(Options{}).dial(context.Background(), "tcp", "localhost:80"); err == nil {
		t.Fatal("hostname resolving to loopback accepted")
	}
}
