package emailchannel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestVerifySignatureRequiresFreshTimestamp(t *testing.T) {
	secret := "secret-value"
	body := []byte(`{"to":"support@example.com","from":"person@example.com","body":"hello"}`)
	validTimestamp := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(formatSigned(validTimestamp, body)))
	header := "t=" + formatInt(validTimestamp) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	if err := verifySignature(body, header, secret); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	old := time.Now().Add(-6 * time.Minute).Unix()
	mac = hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(formatSigned(old, body)))
	if err := verifySignature(body, "t="+formatInt(old)+",v1="+hex.EncodeToString(mac.Sum(nil)), secret); err == nil {
		t.Fatal("stale signature accepted")
	}
}

func TestMailboxSafetyHelpers(t *testing.T) {
	if got, err := normalizeAddress("Support <Support@Example.com>"); err != nil || got != "support@example.com" {
		t.Fatalf("normalize address: %q, %v", got, err)
	}
	if !senderAllowed("person@example.com", []string{"example.com"}, nil) {
		t.Fatal("allowed domain rejected")
	}
	if senderAllowed("blocked@example.com", nil, []string{"blocked@example.com"}) {
		t.Fatal("blocked sender accepted")
	}
	if got := stripQuotedText("new reply\n\n> old reply\nmore"); got != "new reply" {
		t.Fatalf("quote stripping: %q", got)
	}
}

func TestUnmarshalProviderPayload(t *testing.T) {
	input, err := UnmarshalProviderPayload([]byte(`{"to":["support@example.com"],"from":"person@example.com","text":"hello","MessageID":"<m-1>"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(input.To) != 1 || input.To[0] != "support@example.com" || input.Body != "hello" || input.MessageID != "<m-1>" {
		t.Fatalf("unexpected payload: %+v", input)
	}
}

func TestOutboundHeaderUsesMailboxDomain(t *testing.T) {
	if got := outboundHeader("msg_123", "Support <support@Example.com>"); got != "<msg_123@Example.com>" {
		t.Fatalf("outbound header: %q", got)
	}
	if got := outboundHeader("msg_123", "not-an-address"); got != "<msg_123@hubchat.invalid>" {
		t.Fatalf("invalid outbound header fallback: %q", got)
	}
}

func TestOutboundPayloadIsStable(t *testing.T) {
	payload := outboundPayload{
		WorkspaceID: "ws_1", EmailMessageID: "em_1", To: "person@example.com",
		Subject: "Re: Help", Body: "We fixed it", ReplyTo: "support@example.com",
	}
	if payload.EmailMessageID == "" || payload.WorkspaceID == "" || payload.ReplyTo == "" {
		t.Fatal("outbound payload lost durable routing fields")
	}
}

func formatSigned(timestamp int64, body []byte) string {
	return formatInt(timestamp) + "." + string(body)
}

func formatInt(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	buf := make([]byte, 0, 20)
	for value > 0 {
		buf = append([]byte{byte('0' + value%10)}, buf...)
		value /= 10
	}
	if negative {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
