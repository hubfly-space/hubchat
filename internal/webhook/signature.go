package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidSignature = errors.New("webhook: invalid signature")
	ErrReplay           = errors.New("webhook: signature timestamp outside tolerance")
)

// Signature creates the versioned HMAC value sent with an outbound webhook.
// The timestamp is part of the signed material, so a captured body cannot be
// replayed indefinitely even when the endpoint's secret remains unchanged.
func Signature(secret, payload []byte, timestamp time.Time) string {
	material := fmt.Sprintf("%d.%s", timestamp.Unix(), payload)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(material))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a v1 signature and rejects stale/future timestamps. Callers
// should pass the raw request body, never a re-marshalled JSON object.
func Verify(secret, payload []byte, timestamp string, signature string, now time.Time, tolerance time.Duration) error {
	seconds, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil || tolerance < 0 {
		return ErrInvalidSignature
	}
	when := time.Unix(seconds, 0)
	if delta := now.Sub(when); delta < -tolerance || delta > tolerance {
		return ErrReplay
	}
	expected := Signature(secret, payload, when)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(strings.TrimSpace(signature))) != 1 {
		return ErrInvalidSignature
	}
	return nil
}
