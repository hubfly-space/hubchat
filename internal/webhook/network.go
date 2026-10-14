package webhook

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Options is deployment policy, never a workspace-controlled setting.
type Options struct{ AllowedPrivateHosts []string }

type networkPolicy struct{ privateHosts map[string]bool }

func newNetworkPolicy(options Options) networkPolicy {
	p := networkPolicy{privateHosts: make(map[string]bool)}
	for _, host := range options.AllowedPrivateHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" && !strings.ContainsAny(host, "/*@") {
			p.privateHosts[host] = true
		}
	}
	return p
}

func restrictedIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return true
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4"} {
		if netip.MustParsePrefix(prefix).Contains(addr) {
			return true
		}
	}
	return false
}

func (p networkPolicy) client() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy could resolve a destination itself and evade the checked dial.
	transport.Proxy = nil
	transport.DialContext = p.dial
	return &http.Client{Timeout: 10 * time.Second, Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (p networkPolicy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	host = strings.ToLower(host)
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("webhook: destination has no addresses")
	}
	// Fail closed for mixed public/private answers. Dial the verified IP itself
	// so a second resolution cannot redirect the connection through DNS rebinding.
	for _, ip := range addresses {
		if restrictedIP(ip) && !p.privateHosts[host] {
			return nil, fmt.Errorf("webhook: destination is blocked by deployment network policy")
		}
	}
	var last error
	for _, ip := range addresses {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

func (s *Service) validateURL(raw string) (string, error) {
	normalized, err := validateURL(raw)
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(normalized)
	host := strings.ToLower(parsed.Hostname())
	if strings.HasSuffix(host, ".") || parsed.Fragment != "" {
		return "", ErrInvalidURL
	}
	if ip, err := netip.ParseAddr(host); err == nil && restrictedIP(ip) && !s.network.privateHosts[host] {
		return "", fmt.Errorf("%w: private destinations require a deployment allowlist", ErrInvalidURL)
	}
	return normalized, nil
}

// NewHTTPClient applies the same checked-IP policy to other outbound adapters.
func NewHTTPClient(allowedPrivateHosts []string) *http.Client {
	return newNetworkPolicy(Options{AllowedPrivateHosts: allowedPrivateHosts}).client()
}
