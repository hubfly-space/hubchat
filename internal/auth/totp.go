package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Time-based one-time passwords, RFC 6238.
//
// Implemented here rather than pulled in as a dependency. The algorithm is
// forty lines of HMAC and it has not changed since 2011; the surface area of
// an extra module in the supply chain of an authentication path is a worse
// trade than the code below. CONTRIBUTING.md asks for new dependencies to be
// justified, and "saves forty lines" does not clear that bar for something
// every sign-in touches.
//
// SHA-1 is not a weakness here despite its reputation for collisions. HMAC's
// security does not rest on collision resistance, and every authenticator app
// in circulation — Google Authenticator, 1Password, Aegis, Bitwarden — assumes
// SHA-1. Choosing SHA-256 would be cosmetically stronger and practically
// broken, because most users could not enrol.

const (
	// totpDigits is fixed at 6: every authenticator app expects it, and the
	// brute-force resistance comes from the attempt limit, not the length.
	totpDigits = 6
	// totpPeriod is the 30-second step every app assumes.
	totpPeriod = 30 * time.Second
	// totpSkew allows one step either side, so a code entered as it rolls over
	// still works. Wider windows multiply the guess space for no real gain.
	totpSkew = 1
)

// secretEncoding is base32 without padding — what authenticator apps read from
// a QR code and what users type when scanning fails.
var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret generates a 20-byte secret, base32-encoded.
//
// Twenty bytes is what RFC 4226 recommends and what the HMAC-SHA1 block size
// makes natural; longer secrets are silently truncated by some apps.
func NewTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate totp secret: %w", err)
	}
	return secretEncoding.EncodeToString(buf), nil
}

// TOTPProvisioningURI builds the otpauth:// URI an authenticator app scans.
//
// `issuer` appears twice by design: once as a path prefix and once as a
// parameter. Older apps read one, newer ones read the other, and an account
// that shows up as a bare email address with no product name next to it is one
// the user cannot identify six months later.
func TOTPProvisioningURI(secret, accountEmail, issuer string) string {
	label := url.PathEscape(issuer + ":" + accountEmail)

	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", issuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprint(totpDigits))
	params.Set("period", fmt.Sprint(int(totpPeriod.Seconds())))

	return "otpauth://totp/" + label + "?" + params.Encode()
}

// VerifyTOTP reports whether code is valid for secret at time now.
//
// Comparison is constant-time. A timing-variable compare on a six-digit code
// is a real oracle: an attacker who can measure how far into the string the
// comparison failed reduces a million guesses to sixty.
func VerifyTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}

	key, err := secretEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return false
	}

	counter := uint64(now.Unix()) / uint64(totpPeriod.Seconds())

	for offset := -totpSkew; offset <= totpSkew; offset++ {
		candidate := generateTOTP(key, uint64(int64(counter)+int64(offset)))
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// GenerateTOTP returns the code an authenticator app would show for secret at
// time now.
//
// Exported because verification alone is not enough to test against, and
// because `hubchat doctor` benefits from being able to show an operator the
// code their own server expects when 2FA enrolment is going wrong. It grants
// nothing a holder of the secret could not already compute.
func GenerateTOTP(secret string, now time.Time) (string, error) {
	key, err := secretEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("auth: decode totp secret: %w", err)
	}
	counter := uint64(now.Unix()) / uint64(totpPeriod.Seconds())
	return generateTOTP(key, counter), nil
}

// generateTOTP is the HOTP truncation of RFC 4226 §5.3.
func generateTOTP(key []byte, counter uint64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(message[:])
	sum := mac.Sum(nil)

	// The last nibble selects where in the digest to read from, so an attacker
	// who learns one code cannot infer the digest's other bytes.
	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", totpDigits, truncated%pow10(totpDigits))
}

func pow10(n int) uint32 {
	result := uint32(1)
	for range n {
		result *= 10
	}
	return result
}

// NewRecoveryCodes generates single-use codes for when the authenticator is
// lost.
//
// Formatted in two groups of five with a hyphen because they are transcribed
// by hand from a printout, and an unbroken ten-character string is where
// transcription errors come from. Base32's alphabet is used for the same
// reason: no 0/O or 1/I ambiguity.
func NewRecoveryCodes(count int) ([]string, error) {
	codes := make([]string, 0, count)
	for range count {
		buf := make([]byte, 10)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("auth: generate recovery code: %w", err)
		}
		raw := secretEncoding.EncodeToString(buf)[:10]
		codes = append(codes, strings.ToLower(raw[:5]+"-"+raw[5:]))
	}
	return codes, nil
}

// NormalizeRecoveryCode makes a hand-typed code comparable to a stored hash:
// case and separators are what users get wrong, and neither carries meaning.
func NormalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
