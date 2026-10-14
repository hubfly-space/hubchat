//go:build integration

package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/realtime"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestWebSocketRequiresConversationReadAndWorkspaceSSO(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,email,password_hash) VALUES('usr_ws_policy','WS policy','ws-policy@example.com','unused'); INSERT INTO workspaces(id,name,slug) VALUES('wrk_ws_policy','WS policy','ws-policy'); INSERT INTO workspace_members(id,workspace_id,user_id,role) VALUES('mem_ws_policy','wrk_ws_policy','usr_ws_policy','owner')`); err != nil {
		t.Fatal(err)
	}
	authService := auth.New(pool, auth.Options{SessionLifetime: time.Hour})
	workspaceService := workspace.New(pool, nil, nil)
	session, err := authService.CreateSessionWithMethod(ctx, "usr_ws_policy", "ws-test", "", auth.AuthMethodPassword)
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{Auth: authService, Workspace: workspaceService, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	role, err := workspaceService.CreateRole(ctx, "wrk_ws_policy", "mem_ws_policy", workspace.RoleInput{Key: "ws_no_read", Name: "No read", Capabilities: []authorization.Capability{authorization.TaskManage}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workspace_members SET role=$1 WHERE id='mem_ws_policy'`, role.Key); err != nil {
		t.Fatal(err)
	}
	handler := NewWebSocketHandler(deps, realtime.NewHub(deps.Logger, 10))
	request := func() int {
		req := httptest.NewRequest(http.MethodGet, "/conversations?workspace_id=wrk_ws_policy", nil)
		req.AddCookie(&http.Cookie{Name: httpserver.SessionCookieName, Value: session.Token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}
	if status := request(); status != http.StatusForbidden {
		t.Fatalf("role without read got %d", status)
	}
	if _, err := pool.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE id='mem_ws_policy'; UPDATE workspaces SET settings='{"security":{"require_sso":true}}' WHERE id='wrk_ws_policy'`); err != nil {
		t.Fatal(err)
	}
	if status := request(); status != http.StatusUnauthorized {
		t.Fatalf("password under SSO got %d", status)
	}
}

func TestRealtimeGrantRechecksRevocationAndCapabilities(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,email,password_hash) VALUES('usr_ws_revoke','WS revoke','ws-revoke@example.com','unused'); INSERT INTO workspaces(id,name,slug) VALUES('wrk_ws_revoke','WS revoke','ws-revoke'); INSERT INTO workspace_members(id,workspace_id,user_id,role) VALUES('mem_ws_revoke','wrk_ws_revoke','usr_ws_revoke','agent'); INSERT INTO inboxes(id,workspace_id,name,slug) VALUES('inb_ws_revoke','wrk_ws_revoke','Inbox','inbox'); INSERT INTO conversations(id,workspace_id,inbox_id,channel) VALUES('cnv_ws_revoke','wrk_ws_revoke','inb_ws_revoke','manual')`); err != nil {
		t.Fatal(err)
	}
	a := auth.New(pool, auth.Options{SessionLifetime: time.Hour})
	w := workspace.New(pool, nil, nil)
	session, err := a.CreateSessionWithMethod(ctx, "usr_ws_revoke", "ws-test", "", auth.AuthMethodPassword)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := w.ActorForUser(ctx, "wrk_ws_revoke", "usr_ws_revoke")
	if err != nil {
		t.Fatal(err)
	}
	grant := agentRealtimeGrant(context.Background(), Deps{Auth: a, Workspace: w, Conversation: conversation.New(pool, nil, nil)}, session.Token, actor)
	if projected, allowed := grant.Filter(events.Record{EntityType: "role"}); allowed && (projected.EntityType != "" || projected.Type != "workspace.visibility_changed") {
		t.Fatal("role details leaked to agent")
	}
	if _, allowed := grant.Filter(events.Record{EntityType: "conversation", EntityID: "cnv_ws_revoke"}); !allowed {
		t.Fatal("conversation denied")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM workspace_members WHERE id='mem_ws_revoke'`); err != nil {
		t.Fatal(err)
	}
	if grant.Valid() {
		t.Fatal("revoked membership remains valid")
	}
	if _, allowed := grant.Filter(events.Record{EntityType: "conversation", EntityID: "cnv_ws_revoke"}); allowed {
		t.Fatal("revoked membership received event")
	}
}
