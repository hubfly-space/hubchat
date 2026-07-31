package emailchannel

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
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

func TestUnmarshalPostmarkInboundPayload(t *testing.T) {
	input, err := UnmarshalProviderPayloadFor("postmark", "application/json", []byte(`{
		"From":"Person <person@example.com>",
		"To":"Support <support@example.com>",
		"Subject":"Re: Help",
		"MessageID":"provider-1",
		"TextBody":"new reply",
		"Headers":[{"Name":"In-Reply-To","Value":"<old@example.com>"},{"Name":"References","Value":"<old@example.com> <root@example.com>"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(input.To) != 1 || input.To[0] != "support@example.com" || input.From != "Person <person@example.com>" || input.Body != "new reply" || input.InReplyTo != "<old@example.com>" || len(input.References) != 2 {
		t.Fatalf("unexpected Postmark payload: %+v", input)
	}
}

func TestUnmarshalJSONInboundAttachments(t *testing.T) {
	input, err := UnmarshalProviderPayloadFor("postmark", "application/json", []byte(`{
		"to":["support@example.com"],
		"from":"person@example.com",
		"text":"see attached",
		"Attachments":[{"Name":"log.txt","ContentType":"text/plain","Content":"aGVsbG8="}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Attachments) != 1 || input.Attachments[0].Name != "log.txt" || input.Attachments[0].MIMEType != "text/plain" || string(input.Attachments[0].Body) != "hello" {
		t.Fatalf("unexpected JSON attachment: %+v", input.Attachments)
	}
}

func TestUnmarshalMultipartInboundAttachment(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("recipient", "support@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("sender", "person@example.com"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("attachment-1", "log.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	input, err := UnmarshalProviderPayloadFor("mailgun", writer.FormDataContentType(), body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Attachments) != 1 || input.Attachments[0].Name != "log.txt" || string(input.Attachments[0].Body) != "hello" {
		t.Fatalf("unexpected multipart attachment: %+v", input.Attachments)
	}
}

func TestParseIMAPMessageExtractsThreadingAndAttachment(t *testing.T) {
	raw := []byte("From: Person <person@example.com>\r\n" +
		"To: support@example.com\r\n" +
		"Subject: Re: Help\r\n" +
		"Message-ID: <incoming-2@example.com>\r\n" +
		"In-Reply-To: <outgoing-1@example.com>\r\n" +
		"References: <root@example.com> <outgoing-1@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=hubchat-test\r\n\r\n" +
		"--hubchat-test\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\n" +
		"new reply\r\n" +
		"--hubchat-test\r\n" +
		"Content-Type: text/plain\r\n" +
		"Content-Disposition: attachment; filename=log.txt\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		"aGVsbG8=\r\n" +
		"--hubchat-test--\r\n")
	input, err := parseIMAPMessage(raw, "support@example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if input.From != "person@example.com" || input.MessageID != "<incoming-2@example.com>" || input.InReplyTo != "<outgoing-1@example.com>" || input.Body != "new reply" {
		t.Fatalf("unexpected IMAP message: %+v", input)
	}
	if len(input.References) != 2 || len(input.Attachments) != 1 || string(input.Attachments[0].Body) != "hello" {
		t.Fatalf("unexpected IMAP threading or attachment data: %+v", input)
	}
}

func TestUnmarshalFormInboundPayload(t *testing.T) {
	input, err := UnmarshalProviderPayloadFor("mailgun", "application/x-www-form-urlencoded", []byte("recipient=support%40example.com&sender=person%40example.com&subject=Hello&body-plain=reply&Message-Id=%3Cm1%40example.com%3E&In-Reply-To=%3Cold%40example.com%3E"))
	if err != nil {
		t.Fatal(err)
	}
	if len(input.To) != 1 || input.To[0] != "support@example.com" || input.From != "person@example.com" || input.Body != "reply" || input.MessageID != "<m1@example.com>" || input.InReplyTo != "<old@example.com>" {
		t.Fatalf("unexpected form payload: %+v", input)
	}
}

func TestUnmarshalDeliveryPayloadNormalizesHardBounce(t *testing.T) {
	event, err := UnmarshalDeliveryPayload("postmark", "application/json", []byte(`{
		"RecordType":"Bounce",
		"Type":"HardBounce",
		"Email":"person@example.com",
		"MessageID":"provider-1",
		"Description":"mailbox does not exist"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != "bounced" || !event.Hard || event.ProviderEventID != "provider-1" || event.Recipient != "person@example.com" {
		t.Fatalf("unexpected delivery event: %+v", event)
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
