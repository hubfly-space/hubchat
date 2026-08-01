//go:build integration

package notification

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/hubchat/hubchat/internal/workspace"
)

type replyTestWorkspace struct {
	id      string
	inboxID string
}

func seedReplyWorkspace(t *testing.T, ctx context.Context, pool *database.Pool, label string) replyTestWorkspace {
	t.Helper()
	userID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id,name,email,password_hash,email_verified_at)
		VALUES ($1,$2,$3,'x',now())
	`, userID, "Reply Agent", userID+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	created, err := workspaceService.Bootstrap(ctx, userID, "Reply Workspace", "reply-"+label+"-"+userID[len(userID)-8:])
	if err != nil {
		t.Fatalf("bootstrap workspace: %v", err)
	}
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1`, created.ID).Scan(&inboxID); err != nil {
		t.Fatalf("find inbox: %v", err)
	}
	return replyTestWorkspace{id: created.ID, inboxID: inboxID}
}

func seedReplyCustomer(t *testing.T, ctx context.Context, pool *database.Pool, workspaceID, email string) string {
	t.Helper()
	id := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `
		INSERT INTO customers (id,workspace_id,name,email,verification)
		VALUES ($1,$2,'Ada Lovelace',$3,'verified')
	`, id, workspaceID, email); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return id
}

func TestCustomerReplyEmailEventConsumer(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)

	ws := seedReplyWorkspace(t, ctx, pool, "reply")
	customerID := seedReplyCustomer(t, ctx, pool, ws.id, "ada@example.com")
	customer := customerID
	eventLog := events.New(pool)
	conversationService := conversation.New(pool, eventLog, audit.New(pool))
	thread, _, err := conversationService.Start(ctx, ws.id, ws.inboxID, "widget", nil, &customer, nil, "Ada Lovelace", "I need help")
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}
	if _, err := ticket.New(pool, workspace.New(pool, eventLog, audit.New(pool)), eventLog, audit.New(pool)).CreateAsCustomer(ctx, ws.id, customerID, "Ada Lovelace", ticket.CreateRequest{
		Title: "Cannot sign in", CustomerID: &customer, InboxID: ws.inboxID, Channel: "widget", ConversationID: &thread.ID,
	}); err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	service := New(pool, jobs.NewClient(pool))
	publicURL, err := url.Parse("https://support.example.test")
	if err != nil {
		t.Fatalf("parse public URL: %v", err)
	}
	service.SetPublicURL(publicURL)
	message := conversation.MessageEvent{
		ConversationID: thread.ID,
		MessageID:      ids.New(ids.PrefixMessage),
		Kind:           "reply",
		AuthorType:     "agent",
		AuthorName:     "Grace Hopper",
		Body:           "I have reset the sign-in link for you.",
	}
	record := events.Record{ID: "evt-reply-1", WorkspaceID: ws.id, Type: events.MessageCreated, Data: mustJSON(t, message)}
	if err := service.processEvent(ctx, record); err != nil {
		t.Fatalf("process reply event: %v", err)
	}
	assertEmailJob(t, ctx, pool, 1, "ada@example.com", "New reply on ticket SUP-1", "Grace Hopper replied")
	if err := service.processEvent(ctx, record); err != nil {
		t.Fatalf("reprocess reply event: %v", err)
	}
	assertEmailJob(t, ctx, pool, 1, "ada@example.com", "New reply on ticket SUP-1", "Grace Hopper replied")

	// A different workspace cannot resolve the conversation and therefore
	// cannot enqueue a job for it.
	other := seedReplyWorkspace(t, ctx, pool, "other")
	wrongWorkspace := record
	wrongWorkspace.ID = "evt-reply-wrong-workspace"
	wrongWorkspace.WorkspaceID = other.id
	if err := service.processEvent(ctx, wrongWorkspace); err != nil {
		t.Fatalf("process wrong-workspace event: %v", err)
	}
	assertEmailJob(t, ctx, pool, 1, "ada@example.com", "New reply on ticket SUP-1", "Grace Hopper replied")

	if _, err := pool.Exec(ctx, `
		INSERT INTO email_suppressions(workspace_id,address,reason,source)
		VALUES ($1,$2,'hard bounce','integration')
	`, ws.id, "ada@example.com"); err != nil {
		t.Fatalf("suppress customer: %v", err)
	}
	suppressed := record
	suppressed.ID = "evt-reply-suppressed"
	if err := service.processEvent(ctx, suppressed); err != nil {
		t.Fatalf("process suppressed event: %v", err)
	}
	assertEmailJob(t, ctx, pool, 1, "ada@example.com", "New reply on ticket SUP-1", "Grace Hopper replied")

	// Email conversations remain owned by emailchannel, which supplies
	// Message-ID/In-Reply-To headers and attachment handling.
	otherCustomer := seedReplyCustomer(t, ctx, pool, ws.id, "email-channel@example.com")
	emailCustomer := otherCustomer
	emailThread, _, err := conversationService.Start(ctx, ws.id, ws.inboxID, "email", nil, &emailCustomer, nil, "Email Customer", "Email question")
	if err != nil {
		t.Fatalf("start email conversation: %v", err)
	}
	if _, err := ticket.New(pool, workspace.New(pool, eventLog, audit.New(pool)), eventLog, audit.New(pool)).CreateAsCustomer(ctx, ws.id, otherCustomer, "Email Customer", ticket.CreateRequest{
		Title: "Email question", CustomerID: &emailCustomer, InboxID: ws.inboxID, Channel: "email", ConversationID: &emailThread.ID,
	}); err != nil {
		t.Fatalf("create email ticket: %v", err)
	}
	emailRecord := events.Record{ID: "evt-email-reply", WorkspaceID: ws.id, Type: events.MessageCreated, Data: mustJSON(t, conversation.MessageEvent{
		ConversationID: emailThread.ID, MessageID: ids.New(ids.PrefixMessage), Kind: "reply", AuthorType: "agent", AuthorName: "Grace Hopper", Body: "Email reply",
	})}
	if err := service.processEvent(ctx, emailRecord); err != nil {
		t.Fatalf("process email-channel event: %v", err)
	}
	assertEmailJob(t, ctx, pool, 1, "ada@example.com", "New reply on ticket SUP-1", "Grace Hopper replied")
}

func assertEmailJob(t *testing.T, ctx context.Context, pool *database.Pool, expected int, recipient, subject, bodyPart string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE type='email.send' AND state='pending'`).Scan(&count); err != nil {
		t.Fatalf("count email jobs: %v", err)
	}
	if count != expected {
		t.Fatalf("email job count = %d, want %d", count, expected)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM jobs WHERE type='email.send' AND state='pending' ORDER BY created_at,id LIMIT 1`).Scan(&payload); err != nil {
		t.Fatalf("read email job: %v", err)
	}
	var message emailPayload
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode email job: %v", err)
	}
	if message.To != recipient || message.Subject != subject {
		t.Fatalf("email payload = %+v", message)
	}
	if !strings.Contains(message.Body, bodyPart) || !strings.Contains(message.Body, "/portal/tickets/") {
		t.Fatalf("email body = %q", message.Body)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test data: %v", err)
	}
	return data
}
