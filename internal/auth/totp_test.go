package auth

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B publishes test vectors for the SHA-1 variant, keyed with
// the ASCII string "12345678901234567890". Checking against them is the only
// way to know this implementation is compatible with every authenticator app
// rather than merely self-consistent.
func TestVerifyTOTPMatchesRFC6238Vectors(t *testing.T) {
	key := secretEncoding.EncodeToString([]byte("12345678901234567890"))

	vectors := []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}

	for _, vector := range vectors {
		at := time.Unix(vector.unix, 0)
		if !VerifyTOTP(key, vector.code, at) {
			t.Errorf("code %s rejected at unix %d; RFC 6238 says it is valid",
				vector.code, vector.unix)
		}
	}
}

func TestVerifyTOTPRejectsWrongCode(t *testing.T) {
	key := secretEncoding.EncodeToString([]byte("12345678901234567890"))
	at := time.Unix(59, 0)

	if VerifyTOTP(key, "000000", at) {
		t.Fatal("an incorrect code was accepted")
	}
}

// One step either side is allowed so a code entered as it rolls over still
// works; anything further must not.
func TestVerifyTOTPAllowsOneStepOfSkewOnly(t *testing.T) {
	key := secretEncoding.EncodeToString([]byte("12345678901234567890"))

	// 287082 is the code for the step containing unix 59.
	const code = "287082"

	if !VerifyTOTP(key, code, time.Unix(59+30, 0)) {
		t.Error("code rejected one step late; a user typing as the code rolls over would fail")
	}
	if !VerifyTOTP(key, code, time.Unix(59-30, 0)) {
		t.Error("code rejected one step early")
	}
	if VerifyTOTP(key, code, time.Unix(59+120, 0)) {
		t.Error("a code four steps stale was accepted; the guess window is too wide")
	}
}

func TestVerifyTOTPRejectsMalformedInput(t *testing.T) {
	key := secretEncoding.EncodeToString([]byte("12345678901234567890"))
	at := time.Unix(59, 0)

	for _, code := range []string{"", "12345", "1234567", "abcdef", "  "} {
		if VerifyTOTP(key, code, at) {
			t.Errorf("malformed code %q was accepted", code)
		}
	}

	if VerifyTOTP("not-valid-base32!!", "287082", at) {
		t.Error("a code was accepted against an undecodable secret")
	}
}

// Whitespace around a pasted code is the user's clipboard, not an attack.
func TestVerifyTOTPTrimsSurroundingWhitespace(t *testing.T) {
	key := secretEncoding.EncodeToString([]byte("12345678901234567890"))
	if !VerifyTOTP(key, "  287082  ", time.Unix(59, 0)) {
		t.Fatal("a pasted code with surrounding whitespace was rejected")
	}
}

func TestNewTOTPSecretIsDecodableAndUnique(t *testing.T) {
	seen := make(map[string]bool)

	for range 32 {
		secret, err := NewTOTPSecret()
		if err != nil {
			t.Fatalf("generate secret: %v", err)
		}
		if seen[secret] {
			t.Fatal("the same secret was generated twice")
		}
		seen[secret] = true

		decoded, err := secretEncoding.DecodeString(secret)
		if err != nil {
			t.Fatalf("secret %q is not decodable base32: %v", secret, err)
		}
		if len(decoded) != 20 {
			t.Fatalf("secret decodes to %d bytes, want 20", len(decoded))
		}
	}
}

// The URI has to survive being turned into a QR code and read by an app that
// we do not control, so its shape is part of the contract.
func TestTOTPProvisioningURIShape(t *testing.T) {
	uri := TOTPProvisioningURI("JBSWY3DPEHPK3PXP", "ada@example.com", "Hubchat")

	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("uri does not start with the otpauth scheme: %s", uri)
	}
	for _, want := range []string{
		"secret=JBSWY3DPEHPK3PXP",
		"issuer=Hubchat",
		"algorithm=SHA1",
		"digits=6",
		"period=30",
	} {
		if !strings.Contains(uri, want) {
			t.Errorf("uri is missing %q: %s", want, uri)
		}
	}
	// The label carries the issuer too, for apps that ignore the parameter.
	// The colon stays unescaped: it is legal in a path segment, and the
	// otpauth spec defines the label as literally "issuer:account".
	if !strings.Contains(uri, "otpauth://totp/Hubchat:ada@example.com?") {
		t.Errorf("uri label does not namespace the account by issuer: %s", uri)
	}
}

func TestNewRecoveryCodesAreUniqueAndReadable(t *testing.T) {
	codes, err := NewRecoveryCodes(10)
	if err != nil {
		t.Fatalf("generate recovery codes: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d codes, want 10", len(codes))
	}

	seen := make(map[string]bool)
	for _, code := range codes {
		if seen[code] {
			t.Fatalf("duplicate recovery code %q", code)
		}
		seen[code] = true

		if len(code) != 11 || code[5] != '-' {
			t.Fatalf("code %q is not in the xxxxx-xxxxx form users transcribe", code)
		}
		// Base32's alphabet avoids 0/O and 1/I, which is the whole point for a
		// code read off a printout.
		if strings.ContainsAny(code, "018") {
			t.Fatalf("code %q contains a digit that is ambiguous when handwritten", code)
		}
	}
}

func TestNormalizeRecoveryCodeIgnoresFormatting(t *testing.T) {
	want := "abcde12345"
	for _, input := range []string{"abcde-12345", "ABCDE-12345", " abcde 12345 ", "AbCdE-12345"} {
		if got := NormalizeRecoveryCode(input); got != want {
			t.Errorf("NormalizeRecoveryCode(%q) = %q, want %q", input, got, want)
		}
	}
}

var _ = base32.StdEncoding
