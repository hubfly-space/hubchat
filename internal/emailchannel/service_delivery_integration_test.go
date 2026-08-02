//go:build integration

package emailchannel_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/emailchannel"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/inbox"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestInboundThreadingAndDeliveryLinkage(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	workspaceID, inboxID := seedEmailWorkspace(t, ctx, pool)
	eventLog := events.New(pool)
	auditLog := audit.New(pool)
	conversationService := conversation.New(pool, eventLog, auditLog)
	customerService := customer.New(pool, eventLog, auditLog, config.Default().Limits)
	inboxService := inbox.New(pool, eventLog, auditLog)
	service := emailchannel.New(pool, []byte("integration-email-secret"), conversationService, customerService, inboxService, jobs.NewClient(pool))

	created, err := service.Create(ctx, workspaceID, emailchannel.CreateInput{
		InboxID: inboxID, Address: "support@example.com", InboundMode: "webhook", Enabled: true,
	})
	if err != nil {
		t.Fatalf("create mailbox: %v", err)
	}
	if created.InboundSecret == "" {
		t.Fatal("mailbox did not return its one-time inbound secret")
	}

	firstRaw := []byte(`{"provider":"generic","message_id":"<in-1@example.com>"}`)
	first, err := service.Ingest(ctx, firstRaw, signEmailPayload(firstRaw, created.InboundSecret), emailchannel.InboundEmail{
		To: []string{"support@example.com"}, From: "Ada Lovelace <ada@example.com>", Subject: "Cannot sign in",
		Body: "The sign-in link is not working.", MessageID: "<in-1@example.com>",
	})
	if err != nil {
		t.Fatalf("ingest first email: %v", err)
	}
	if !first.Created || first.ConversationID == "" || first.MessageID == "" {
		t.Fatalf("first ingest result = %+v", first)
	}

	duplicate, err := service.Ingest(ctx, firstRaw, signEmailPayload(firstRaw, created.InboundSecret), emailchannel.InboundEmail{
		To: []string{"support@example.com"}, From: "ada@example.com", Body: "duplicate", MessageID: "<in-1@example.com>",
	})
	if err != emailchannel.ErrDuplicateMessage || duplicate.ConversationID != first.ConversationID || duplicate.MessageID != first.MessageID {
		t.Fatalf("duplicate ingest = %+v, err=%v", duplicate, err)
	}

	replyRaw := []byte(`{"provider":"generic","message_id":"<in-2@example.com>"}`)
	reply, err := service.Ingest(ctx, replyRaw, signEmailPayload(replyRaw, created.InboundSecret), emailchannel.InboundEmail{
		To: []string{"support@example.com"}, From: "ada@example.com", Subject: "Re: Cannot sign in",
		Body: "I still cannot sign in.", MessageID: "<in-2@example.com>", InReplyTo: "<in-1@example.com>", References: []string{"<in-1@example.com>"},
	})
	if err != nil {
		t.Fatalf("ingest threaded reply: %v", err)
	}
	if reply.Created || reply.ConversationID != first.ConversationID {
		t.Fatalf("threaded reply result = %+v", reply)
	}

	var messageCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE workspace_id=$1 AND conversation_id=$2`, workspaceID, first.ConversationID).Scan(&messageCount); err != nil {
		t.Fatalf("count threaded messages: %v", err)
	}
	if messageCount != 2 {
		t.Fatalf("threaded message count = %d, want 2", messageCount)
	}

	outboundID := ids.New(ids.PrefixEmailMessage)
	outboundHeader := "<outbound-1@example.com>"
	if _, err := pool.Exec(ctx, `
		INSERT INTO email_messages
			(id,workspace_id,mailbox_id,direction,message_id_header,to_addresses,status)
		VALUES($1,$2,$3,'outbound',$4,$5,'sent')
	`, outboundID, workspaceID, created.Mailbox.ID, outboundHeader, []string{"ada@example.com"}); err != nil {
		t.Fatalf("seed outbound message: %v", err)
	}

	deliveryRaw := []byte(`{"event":"bounced","id":"provider-delivery-1"}`)
	delivery := emailchannel.DeliveryEvent{
		ProviderEventID: "provider-delivery-1", Type: "bounced", MessageID: outboundHeader,
		Recipient: "ada@example.com", BounceType: "hard", Reason: "mailbox unavailable", Hard: true,
	}
	if err := service.IngestDelivery(ctx, created.Mailbox.ID, "generic", deliveryRaw, signEmailPayload(deliveryRaw, created.InboundSecret), delivery); err != nil {
		t.Fatalf("ingest delivery callback: %v", err)
	}

	var status, linkedMessageID, suppressionReason string
	if err := pool.QueryRow(ctx, `SELECT status FROM email_messages WHERE workspace_id=$1 AND id=$2`, workspaceID, outboundID).Scan(&status); err != nil {
		t.Fatalf("read bounced message: %v", err)
	}
	if status != "bounced" {
		t.Fatalf("outbound status = %q, want bounced", status)
	}
	if err := pool.QueryRow(ctx, `SELECT email_message_id FROM email_delivery_events WHERE workspace_id=$1 AND provider_event_id=$2`, workspaceID, delivery.ProviderEventID).Scan(&linkedMessageID); err != nil {
		t.Fatalf("read delivery linkage: %v", err)
	}
	if linkedMessageID != outboundID {
		t.Fatalf("delivery linked to %q, want %q", linkedMessageID, outboundID)
	}
	if err := pool.QueryRow(ctx, `SELECT reason FROM email_suppressions WHERE workspace_id=$1 AND address=$2`, workspaceID, "ada@example.com").Scan(&suppressionReason); err != nil {
		t.Fatalf("read hard-bounce suppression: %v", err)
	}
	if suppressionReason != "mailbox unavailable" {
		t.Fatalf("suppression reason = %q", suppressionReason)
	}

	if err := service.IngestDelivery(ctx, created.Mailbox.ID, "generic", deliveryRaw, signEmailPayload(deliveryRaw, created.InboundSecret), delivery); err != nil {
		t.Fatalf("replay delivery callback: %v", err)
	}
	var deliveryCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM email_delivery_events WHERE workspace_id=$1 AND provider_event_id=$2`, workspaceID, delivery.ProviderEventID).Scan(&deliveryCount); err != nil {
		t.Fatalf("count delivery callbacks: %v", err)
	}
	if deliveryCount != 1 {
		t.Fatalf("delivery callback count = %d, want 1", deliveryCount)
	}
}

func seedEmailWorkspace(t *testing.T, ctx context.Context, pool *database.Pool) (string, string) {
	t.Helper()
	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id,name,email,password_hash,email_verified_at)
		VALUES ($1,'Email Integration Owner',$2,'x',now())
	`, userID, userID+"@example.com"); err != nil {
		t.Fatalf("seed email owner: %v", err)
	}
	eventLog := events.New(pool)
	workspaceService := workspace.New(pool, eventLog, audit.New(pool))
	created, err := workspaceService.Bootstrap(ctx, userID, "Email Integration Workspace", "email-integration-"+userID[len(userID)-8:])
	if err != nil {
		t.Fatalf("bootstrap email workspace: %v", err)
	}
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 ORDER BY created_at,id LIMIT 1`, created.ID).Scan(&inboxID); err != nil {
		t.Fatalf("find email inbox: %v", err)
	}
	return created.ID, inboxID
}

func signEmailPayload(body []byte, secret string) string {
	timestamp := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(fmt.Sprintf("%d.", timestamp)))
	_, _ = mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
}
