package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// NewToken returns a URL-safe random token for session cookies, invite links,
// and password-reset links: 32 bytes of entropy, base64url-encoded.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken returns the value actually stored in the database. Sessions and
// reset tokens are stored as a hash, never as the raw value (§11.5) — a
// database dump must not be a set of live sessions.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// HashTokenHex is a convenience for logging/debug contexts where a stable,
// non-reversible hex form is more useful than raw bytes.
func HashTokenHex(token string) string {
	return hex.EncodeToString(HashToken(token))
}
