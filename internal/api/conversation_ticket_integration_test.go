//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ticket"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestConvertConversationToTicketPreservesTheConversationLink(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id,name,email,password_hash,email_verified_at)
		VALUES ('usr_convert','Convert owner','convert@example.com','x',now())
	`); err != nil {
		t.Fatal(err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	ws, err := workspaceService.Bootstrap(ctx, "usr_convert", "Convert workspace", "convert-workspace")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := workspaceService.ActorForUser(ctx, ws.ID, "usr_convert")
	if err != nil {
		t.Fatal(err)
	}
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 LIMIT 1`, ws.ID).Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	conversationService := conversation.New(pool, events.New(pool), audit.New(pool))
	conv, _, err := conversationService.Start(ctx, ws.ID, inboxID, "widget", nil, nil, nil, "Visitor", "Please track this")
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{
		Conversation: conversationService,
		Ticket:       ticket.New(pool, workspaceService, events.New(pool), audit.New(pool)),
		Audit:        audit.New(pool),
	}
	actorContext := &authorization.Actor{WorkspaceID: ws.ID, MemberID: actor.MemberID, Role: "owner"}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+conv.ID+"/ticket", strings.NewReader(`{}`)).WithContext(authorization.WithActor(ctx, actorContext))
	req.SetPathValue("id", conv.ID)
	response := httptest.NewRecorder()
	handleConvertConversationToTicket(deps)(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("conversion status = %d: %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	ticketID, ok := result["id"].(string)
	if !ok || ticketID == "" {
		t.Fatalf("conversion response missing ticket id: %+v", result)
	}
	var linkedTicketID string
	if err := pool.QueryRow(ctx, `SELECT ticket_id FROM conversations WHERE workspace_id=$1 AND id=$2`, ws.ID, conv.ID).Scan(&linkedTicketID); err != nil {
		t.Fatal(err)
	}
	if linkedTicketID != ticketID {
		t.Fatalf("conversation ticket_id = %s, want %s", linkedTicketID, ticketID)
	}
}
