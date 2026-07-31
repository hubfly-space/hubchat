package mailer

import (
	"strings"
	"testing"
)

func TestComposeIncludesThreadingHeaders(t *testing.T) {
	sender := &SMTPSender{from: "Support <support@example.com>"}
	raw := string(sender.compose(Message{
		To:        "person@example.com",
		Subject:   "Re: Help",
		Body:      "We fixed it",
		MessageID: "<reply-1@example.com>",
		InReplyTo: "<incoming-1@example.com>",
	}))

	for _, want := range []string{
		"Message-ID: <reply-1@example.com>\r\n",
		"In-Reply-To: <incoming-1@example.com>\r\n",
		"References: <incoming-1@example.com>\r\n",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("composed email is missing %q:\n%s", want, raw)
		}
	}
}

func TestComposeSanitizesThreadingHeaders(t *testing.T) {
	sender := &SMTPSender{from: "Support <support@example.com>"}
	raw := string(sender.compose(Message{MessageID: "<safe>\r\nBcc: attacker@example.com>"}))
	if strings.Contains(raw, "\r\nBcc:") {
		t.Fatalf("header injection survived composition:\n%s", raw)
	}
}

func TestComposeIncludesAttachmentsAsMIMEParts(t *testing.T) {
	sender := &SMTPSender{from: "Support <support@example.com>"}
	raw := string(sender.compose(Message{
		To:   "person@example.com",
		Body: "See the log",
		Attachments: []Attachment{{
			Name: "log.txt", MIMEType: "text/plain", Body: []byte("hello"),
		}},
	}))
	for _, want := range []string{
		"Content-Type: multipart/mixed; boundary=",
		"Content-Disposition: attachment; filename=log.txt",
		"Content-Transfer-Encoding: base64",
		"aGVsbG8=",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("composed multipart email is missing %q:\n%s", want, raw)
		}
	}
}
