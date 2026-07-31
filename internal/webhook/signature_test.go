package webhook

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestSignatureRoundTripAndTamperProtection(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	secret := []byte("endpoint-secret")
	payload := []byte(`{"type":"ticket.created","id":"tkt_1"}`)
	signature := Signature(secret, payload, now)
	if err := Verify(secret, payload, strconvFormat(now.Unix()), signature, now, 5*time.Minute); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := Verify(secret, []byte(`{"type":"ticket.updated"}`), strconvFormat(now.Unix()), signature, now, 5*time.Minute); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tampered payload error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsReplayAndWrongSecret(t *testing.T) {
	now := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	timestamp := strconvFormat(now.Add(-6 * time.Minute).Unix())
	signature := Signature([]byte("secret"), []byte("body"), now.Add(-6*time.Minute))
	if err := Verify([]byte("secret"), []byte("body"), timestamp, signature, now, 5*time.Minute); !errors.Is(err, ErrReplay) {
		t.Fatalf("stale signature error = %v, want ErrReplay", err)
	}
	if err := Verify([]byte("other"), []byte("body"), strconvFormat(now.Unix()), Signature([]byte("secret"), []byte("body"), now), now, 5*time.Minute); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("wrong secret error = %v, want ErrInvalidSignature", err)
	}
}

func strconvFormat(value int64) string {
	return strconv.FormatInt(value, 10)
}
