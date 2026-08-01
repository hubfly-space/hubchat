//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestRequireActorEnforcesWorkspaceSSOBySessionProvenance(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, name, email, password_hash) VALUES
			('usr_sso_policy', 'SSO Policy', 'sso-policy@example.com', 'unused');
		INSERT INTO workspaces (id, name, slug, settings) VALUES
			('wrk_sso_policy', 'SSO Policy', 'sso-policy', '{"security":{"require_sso":true}}');
		INSERT INTO workspace_members (id, workspace_id, user_id, role) VALUES
			('mem_sso_policy', 'wrk_sso_policy', 'usr_sso_policy', 'owner');
	`); err != nil {
		t.Fatal(err)
	}

	authService := auth.New(pool, auth.Options{SessionLifetime: time.Hour})
	workspaceService := workspace.New(pool, nil, nil)
	passwordSession, err := authService.CreateSessionWithMethod(ctx, "usr_sso_policy", "password-browser", "", auth.AuthMethodPassword)
	if err != nil {
		t.Fatal(err)
	}
	oauthSession, err := authService.CreateSessionWithMethod(ctx, "usr_sso_policy", "oauth-browser", "", auth.AuthMethodOAuth)
	if err != nil {
		t.Fatal(err)
	}

	call := func(token string) (int, bool) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", nil)
		req.AddCookie(&http.Cookie{Name: httpserver.SessionCookieName, Value: token})
		req.Header.Set("Hubchat-Workspace-Id", "wrk_sso_policy")
		called := false
		response := httptest.NewRecorder()
		requireActor(Deps{Auth: authService, Workspace: workspaceService}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(response, req)
		return response.Code, called
	}

	if status, called := call(passwordSession.Token); status != http.StatusUnauthorized || called {
		t.Fatalf("password session under required SSO: status=%d called=%v", status, called)
	}
	if status, called := call(oauthSession.Token); status != http.StatusNoContent || !called {
		t.Fatalf("OAuth session under required SSO: status=%d called=%v", status, called)
	}
}
