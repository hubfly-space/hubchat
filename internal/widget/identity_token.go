package widget

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrIdentityTokenInvalid = errors.New("widget: identity token is malformed or has an invalid signature")
	ErrIdentityTokenExpired = errors.New("widget: identity token has expired")
)

// identityClaims is what a workspace's own backend asserts about a visitor,
// signed so the widget can trust it without a round trip to that backend.
type identityClaims struct {
	Subject string `json:"sub"`           // the external id this workspace's own system uses for this person
	Issuer  string `json:"iss"`           // who signed this — informational, shown back on verification failure
	Email   string `json:"email"`
	Name    string `json:"name"`
	Expiry  int64  `json:"exp"`           // unix seconds
	Nonce   string `json:"nonce"`
}

// identityKeyForWorkspace derives a per-workspace signing key from the
// deployment's master secret, rather than storing a second secret per
// workspace. This is the value a workspace's own backend uses to sign
// identity tokens — deterministic so it never needs its own row, and it
// rotates automatically (invalidating every previously issued token) if the
// deployment's master key is ever rotated.
func identityKeyForWorkspace(secretKey []byte, workspaceID string) []byte {
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte("identity-token:" + workspaceID))
	return mac.Sum(nil)
}

// IdentitySecret returns the hex-encoded per-workspace signing key, for
// display in the dashboard's SDK guide. It is recoverable rather than
// write-once because it is deterministic, not a stored credential — showing
// it again costs nothing a database dump would not already reveal.
func (s *Service) IdentitySecret(workspaceID string) string {
	return hex.EncodeToString(identityKeyForWorkspace(s.secretKey, workspaceID))
}

// verifyIdentityToken checks a compact HS256 token — base64url(header).
// base64url(payload).base64url(signature), the same shape a JWT library on
// the integrator's side produces — against the workspace's derived key.
//
// What is actually enforced: the signature (unforgeable without the key),
// and expiry (a stolen token stops working). iss and sub are required to be
// present because an integration that omits them is asserting nothing
// verifiable, but there is no registered-issuer list to check iss against —
// the signature itself is what proves the claim came from this workspace's
// own backend.
//
// nonce is carried and could back a used-token cache to reject replay within
// the expiry window, but no such cache exists yet — a captured, unexpired
// token is currently replayable. Signature forgery and use-after-expiry are
// closed; replay-within-expiry is a known gap, not an oversight.
func verifyIdentityToken(secretKey []byte, workspaceID, token string) (*identityClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrIdentityTokenInvalid
	}

	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrIdentityTokenInvalid
	}

	key := identityKeyForWorkspace(secretKey, workspaceID)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return nil, ErrIdentityTokenInvalid
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrIdentityTokenInvalid
	}
	var claims identityClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrIdentityTokenInvalid
	}
	if claims.Subject == "" || claims.Issuer == "" || claims.Expiry == 0 {
		return nil, ErrIdentityTokenInvalid
	}
	if time.Unix(claims.Expiry, 0).Before(time.Now()) {
		return nil, ErrIdentityTokenExpired
	}
	return &claims, nil
}
