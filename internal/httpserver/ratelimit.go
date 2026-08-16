package httpserver

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimiter is a token bucket per key, held in memory.
//
// In-process rather than in PostgreSQL on purpose. §8.6 lists rate-limit
// counters as something Postgres *can* coordinate, with the caveat "with
// careful design" — and the careful design is the problem: a shared counter
// means a database round trip on every request, including the ones being
// rejected, which is exactly backwards. An attacker sending ten thousand
// requests a second should cost us ten thousand map lookups, not ten thousand
// queries.
//
// The trade-off is that N processes allow N times the configured rate. That is
// acceptable because this limit exists to blunt abuse and runaway clients, not
// to meter billing. When exact global limits matter, the counter moves to
// Postgres and the cost becomes justified.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket

	// capacity is the burst; refill is how many tokens return per second.
	capacity float64
	refill   float64

	// lastSweep bounds memory: idle buckets are dropped periodically so a
	// long-running process does not accumulate one entry per IP it has ever
	// seen.
	lastSweep time.Time
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// NewRateLimiter returns a limiter allowing requestsPerMinute sustained, with
// a burst of the same size.
func NewRateLimiter(requestsPerMinute int) *RateLimiter {
	if requestsPerMinute <= 0 {
		requestsPerMinute = 600
	}
	return &RateLimiter{
		buckets:   make(map[string]*bucket),
		capacity:  float64(requestsPerMinute),
		refill:    float64(requestsPerMinute) / 60,
		lastSweep: time.Now(),
	}
}

// Allow reports whether key may proceed, and how many tokens remain.
func (l *RateLimiter) Allow(key string) (allowed bool, remaining int, retryAfter time.Duration) {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.capacity, lastSeen: now}
		l.buckets[key] = b
	}

	// Refill for the time elapsed, capped at the burst size.
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens = min(l.capacity, b.tokens+elapsed*l.refill)
	b.lastSeen = now

	if b.tokens < 1 {
		// How long until one token is available again.
		wait := time.Duration((1 - b.tokens) / l.refill * float64(time.Second))
		return false, 0, wait
	}

	b.tokens--
	return true, int(b.tokens), 0
}

// sweepLocked drops buckets that have been full and untouched for long enough
// that forgetting them changes nothing.
func (l *RateLimiter) sweepLocked(now time.Time) {
	const (
		sweepInterval = time.Minute
		idleFor       = 10 * time.Minute
	)

	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now

	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) > idleFor {
			delete(l.buckets, key)
		}
	}
}

// RateLimit rejects requests over the configured rate with 429 and the
// X-RateLimit-* headers §16 specifies.
//
// Keying is by client address, which is the only identity available before
// authentication — and pre-authentication is where the limit matters most,
// since that is where brute-force and enumeration happen. Authenticated,
// per-key limits are applied separately once the API key layer exists.
func RateLimit(limiter *RateLimiter, trustedProxies []string) func(http.Handler) http.Handler {
	trusted := parseTrustedProxies(trustedProxies)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := ClientIP(r, trusted)

			allowed, remaining, retryAfter := limiter.Allow(key)

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(int(limiter.capacity)))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if !allowed {
				seconds := int(retryAfter.Seconds()) + 1
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(retryAfter).Unix(), 10))
				WriteError(w, r, http.StatusTooManyRequests, CodeRateLimited,
					"Too many requests. Try again shortly.")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP resolves the caller's address.
//
// X-Forwarded-For is honoured only when the immediate peer is a configured
// trusted proxy. Trusting it unconditionally would let any client set the
// header and defeat both rate limiting and the IP recorded in audit entries —
// a header an attacker controls is not an identity.
func ClientIP(r *http.Request, trusted []net.IPNet) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peer = r.RemoteAddr
	}

	if len(trusted) == 0 || !ipInAny(peer, trusted) {
		return peer
	}

	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		return peer
	}

	// Left-most entry is the original client; everything after it was added by
	// intermediaries.
	first, _, _ := strings.Cut(forwarded, ",")
	forwarded = strings.TrimSpace(first)
	if net.ParseIP(forwarded) == nil {
		return peer
	}
	return forwarded
}

// RequestClientIP applies the same trusted-proxy policy used by rate limiting
// to application features that need the caller address. It never trusts a
// forwarded address supplied directly by an untrusted client.
func RequestClientIP(r *http.Request, trustedProxies []string) string {
	return ClientIP(r, parseTrustedProxies(trustedProxies))
}

func parseTrustedProxies(cidrs []string) []net.IPNet {
	var nets []net.IPNet
	for _, entry := range cidrs {
		if _, network, err := net.ParseCIDR(entry); err == nil {
			nets = append(nets, *network)
			continue
		}
		// A bare address is treated as a single-host range.
		if ip := net.ParseIP(entry); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			nets = append(nets, net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return nets
}

func ipInAny(address string, nets []net.IPNet) bool {
	ip := net.ParseIP(address)
	if ip == nil {
		return false
	}
	for _, network := range nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
