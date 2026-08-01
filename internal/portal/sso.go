package portal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrSSOTokenInvalid = errors.New("portal: invalid SSO token")
	ErrSSOTokenExpired = errors.New("portal: expired SSO token")
	ErrSSOTokenReplay  = errors.New("portal: SSO token has already been used")
	ErrSSONotConfigured = errors.New("portal: SSO is not configured")
)

// SSOClaims is the compact HS256 contract used by a customer's application
// to hand an already-authenticated customer into a portal.  The audience is
// the portal id, so a token cannot be moved between portals even when an
// integration accidentally shares its signing secret.
type SSOClaims struct {
	Subject string `json:"sub"`
	Issuer  string `json:"iss"`
	Audience string `json:"aud"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Nonce   string `json:"nonce"`
	Expiry  int64  `json:"exp"`
}

const ssoTokenLifetimeLimit = 24 * time.Hour

// SignSSOToken is also useful to integration tests and SDK documentation.
// It intentionally emits the small JWT-compatible form that can be produced
// by any standard-library HMAC implementation in the customer's backend.
func SignSSOToken(secret string, claims SSOClaims) (string, error) {
	if strings.TrimSpace(secret) == "" || claims.Subject == "" || claims.Issuer == "" || claims.Audience == "" || claims.Nonce == "" || claims.Expiry <= 0 {
		return "", ErrSSOTokenInvalid
	}
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("portal: encode SSO token: %w", err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	input := encodedHeader + "." + encodedPayload
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifySSOToken(secret, token string, now time.Time) (*SSOClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return nil, ErrSSOTokenInvalid
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || string(header) != `{"alg":"HS256","typ":"JWT"}` {
		return nil, ErrSSOTokenInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrSSOTokenInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, ErrSSOTokenInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrSSOTokenInvalid
	}
	var claims SSOClaims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Subject == "" || claims.Issuer == "" || claims.Audience == "" || claims.Nonce == "" || claims.Expiry <= 0 {
		return nil, ErrSSOTokenInvalid
	}
	expiresAt := time.Unix(claims.Expiry, 0)
	if !expiresAt.After(now) {
		return nil, ErrSSOTokenExpired
	}
	if expiresAt.After(now.Add(ssoTokenLifetimeLimit)) {
		return nil, ErrSSOTokenInvalid
	}
	return &claims, nil
}
