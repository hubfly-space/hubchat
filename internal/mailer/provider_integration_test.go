//go:build provider

package mailer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/config"
)

// TestSMTPProviderSmoke sends through the configured SMTP relay and verifies
// receipt through its inspection endpoint. The default values target the
// MailHog service in docker-compose.yml; production SMTP credentials are
// supplied through the HUBCHAT_PROVIDER_* variables when staging this test.
func TestSMTPProviderSmoke(t *testing.T) {
	if os.Getenv("HUBCHAT_RUN_PROVIDER_TESTS") != "1" {
		t.Skip("set HUBCHAT_RUN_PROVIDER_TESTS=1 to exercise real provider adapters")
	}

	token := time.Now().UTC().Format("20060102-150405.000000000")
	recipient := "provider-smoke-" + strings.ReplaceAll(token, ".", "") + "@example.com"
	subject := "Hubchat provider smoke " + token
	body := "Provider delivery probe " + token

	cfg := config.Email{
		Enabled:      true,
		SMTPHost:     envOr("HUBCHAT_PROVIDER_SMTP_HOST", "127.0.0.1"),
		SMTPPort:     providerSMTPPort(),
		FromAddress:  envOr("HUBCHAT_PROVIDER_SMTP_FROM", "support@example.com"),
		Encryption:   os.Getenv("HUBCHAT_PROVIDER_SMTP_ENCRYPTION"),
		SMTPUsername: os.Getenv("HUBCHAT_PROVIDER_SMTP_USERNAME"),
		SMTPPassword: os.Getenv("HUBCHAT_PROVIDER_SMTP_PASSWORD"),
	}
	sender := New(cfg, slog.Default())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sender.Send(ctx, Message{
		To:      recipient,
		Subject: subject,
		Body:    body,
	}); err != nil {
		t.Fatalf("SMTP send: %v", err)
	}

	apiURL := strings.TrimRight(envOr("HUBCHAT_PROVIDER_SMTP_INSPECTION_URL", "http://127.0.0.1:8025"), "/") + "/api/v2/messages?limit=100"
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		message, err := findProviderMessage(ctx, client, apiURL, recipient, subject, body)
		if err == nil && message {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("SMTP message was not visible at %s", apiURL)
}

type providerMessages struct {
	Items []struct {
		To []struct {
			Mailbox string `json:"Mailbox"`
			Domain  string `json:"Domain"`
		} `json:"To"`
		Content struct {
			Headers map[string][]string `json:"Headers"`
			Body    string              `json:"Body"`
		} `json:"Content"`
	} `json:"items"`
}

func findProviderMessage(ctx context.Context, client *http.Client, apiURL, recipient, subject, body string) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return false, err
	}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return false, fmt.Errorf("inspection endpoint returned %s: %s", response.Status, payload)
	}
	var messages providerMessages
	if err := json.NewDecoder(response.Body).Decode(&messages); err != nil {
		return false, err
	}
	for _, message := range messages.Items {
		toMatch := false
		for _, address := range message.To {
			if address.Mailbox+"@"+address.Domain == recipient {
				toMatch = true
				break
			}
		}
		if !toMatch {
			continue
		}
		headerSubject := ""
		for _, value := range message.Content.Headers["Subject"] {
			headerSubject = value
			break
		}
		if headerSubject == subject && strings.Contains(message.Content.Body, body) {
			return true, nil
		}
	}
	return false, nil
}

func providerSMTPPort() int {
	if raw := os.Getenv("HUBCHAT_PROVIDER_SMTP_PORT"); raw != "" {
		var port int
		if _, err := fmt.Sscanf(raw, "%d", &port); err == nil && port > 0 {
			return port
		}
	}
	return 1025
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
