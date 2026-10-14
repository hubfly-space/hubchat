package push

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	webpush "github.com/SherClockHolmes/webpush-go"
	"testing"
)

func testSubscription(t *testing.T) webpush.Subscription {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return webpush.Subscription{Endpoint: "https://example.test/push/private-token", Keys: webpush.Keys{P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), Auth: base64.RawURLEncoding.EncodeToString(auth)}}
}
func TestSubscriptionsAreValidatedAndEncrypted(t *testing.T) {
	subscription := testSubscription(t)
	if err := validate(subscription); err != nil {
		t.Fatal(err)
	}
	service := New(nil, []byte("local-push-encryption-test"), Options{})
	plain := []byte(subscription.Endpoint)
	encrypted, err := service.encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("private-token")) {
		t.Fatal("subscription stored in plaintext")
	}
	decoded, err := service.decrypt(encrypted)
	if err != nil || !bytes.Equal(decoded, plain) {
		t.Fatalf("decrypt: %v", err)
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := service.decrypt(encrypted); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	subscription.Endpoint = "http://example.test/push"
	if err := validate(subscription); err == nil {
		t.Fatal("insecure subscription accepted")
	}
}
