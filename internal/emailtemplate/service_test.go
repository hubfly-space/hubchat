package emailtemplate

import (
	"strings"
	"testing"
)

func TestValidateAllowsOnlyKnownSimpleVariables(t *testing.T) {
	if err := Validate("agent_replied", Input{Subject: "Reply {{.TicketNumber}}", Body: "Hi {{.CustomerName}}: {{.MessageBody}}"}); err != nil {
		t.Fatalf("expected valid template: %v", err)
	}
	for _, input := range []Input{
		{Subject: "{{.Unknown}}", Body: "body"},
		{Subject: "{{if .CustomerName}}yes{{end}}", Body: "body"},
		{Subject: "bad\nsubject", Body: "body"},
	} {
		if err := Validate("agent_replied", input); err == nil {
			t.Fatalf("expected invalid template for %#v", input)
		}
	}
}

func TestRenderText(t *testing.T) {
	subject, body, err := RenderText("Hello {{.CustomerName}}", "Ticket {{.TicketNumber}}", map[string]string{"CustomerName": "Ada", "TicketNumber": "HC-42"})
	if err != nil || subject != "Hello Ada" || body != "Ticket HC-42" {
		t.Fatalf("unexpected render: subject=%q body=%q err=%v", subject, body, err)
	}
	_, _, err = RenderText("{{.Missing}}", "body", map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "map has no entry") {
		t.Fatalf("expected missing value error, got %v", err)
	}
}
