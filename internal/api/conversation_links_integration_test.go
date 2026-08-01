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
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestConversationLinkRoutesAreIdempotentAndWorkspaceScoped(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id,name,email,password_hash,email_verified_at)
		VALUES ('usr_conversation_links','Link owner','links@example.com','x',now())
	`); err != nil {
		t.Fatal(err)
	}
	workspaceService := workspace.New(pool, events.New(pool), audit.New(pool))
	ws, err := workspaceService.Bootstrap(ctx, "usr_conversation_links", "Link workspace", "conversation-links")
	if err != nil {
		t.Fatal(err)
	}
	actor, err := workspaceService.ActorForUser(ctx, ws.ID, "usr_conversation_links")
	if err != nil {
		t.Fatal(err)
	}
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 LIMIT 1`, ws.ID).Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	service := conversation.New(pool, events.New(pool), audit.New(pool))
	first, _, err := service.Start(ctx, ws.ID, inboxID, "widget", nil, nil, nil, "Visitor", "First")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.Start(ctx, ws.ID, inboxID, "widget", nil, nil, nil, "Visitor", "Second")
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{Conversation: service}
	actorContext := &authorization.Actor{WorkspaceID: ws.ID, MemberID: actor.MemberID, Role: "owner"}
	withActor := func(req *http.Request) *http.Request {
		return req.WithContext(authorization.WithActor(ctx, actorContext))
	}

	create := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+first.ID+"/links", strings.NewReader(`{"target_id":"`+second.ID+`","relation":"related"}`))
	create = withActor(create)
	create.SetPathValue("id", first.ID)
	createdResponse := httptest.NewRecorder()
	handleLinkConversation(deps)(createdResponse, create)
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", createdResponse.Code, createdResponse.Body.String())
	}

	duplicate := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+second.ID+"/links", strings.NewReader(`{"target_id":"`+first.ID+`","relation":"related"}`))
	duplicate = withActor(duplicate)
	duplicate.SetPathValue("id", second.ID)
	duplicateResponse := httptest.NewRecorder()
	handleLinkConversation(deps)(duplicateResponse, duplicate)
	if duplicateResponse.Code != http.StatusConflict {
		t.Fatalf("reverse duplicate status = %d: %s", duplicateResponse.Code, duplicateResponse.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+second.ID+"/links", nil)
	list = withActor(list)
	list.SetPathValue("id", second.ID)
	listResponse := httptest.NewRecorder()
	handleListConversationLinks(deps)(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", listResponse.Code, listResponse.Body.String())
	}
	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("list returned %d links, want 1", len(page.Data))
	}

	remove := httptest.NewRequest(http.MethodDelete, "/api/v1/conversations/"+first.ID+"/links/"+second.ID+"?relation=related", nil)
	remove = withActor(remove)
	remove.SetPathValue("id", first.ID)
	remove.SetPathValue("targetID", second.ID)
	removeResponse := httptest.NewRecorder()
	handleUnlinkConversation(deps)(removeResponse, remove)
	if removeResponse.Code != http.StatusNoContent {
		t.Fatalf("remove status = %d: %s", removeResponse.Code, removeResponse.Body.String())
	}

	otherWorkspace := "wrk_other_conversation_links"
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces (id,name,slug) VALUES ($1,'Other links','other-conversation-links')`, otherWorkspace); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Link(ctx, otherWorkspace, actor.MemberID, first.ID, second.ID, "related"); err == nil {
		t.Fatal("cross-workspace conversation link was accepted")
	}
}
