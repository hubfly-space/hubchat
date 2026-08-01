//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestCustomRoleRoutesUseWorkspaceScopeAndLifecycleRules(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	workspaceService := workspace.New(pool, nil, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ('usr_roles_api','Roles API','roles-api@example.com','unused',now())`); err != nil {
		t.Fatal(err)
	}
	workspaceA, err := workspaceService.Bootstrap(ctx, "usr_roles_api", "Roles A", "roles-a-"+ids.New("x")[len(ids.New("x"))-8:])
	if err != nil {
		t.Fatal(err)
	}
	workspaceB, err := workspaceService.Bootstrap(ctx, "usr_roles_api", "Roles B", "roles-b-"+ids.New("x")[len(ids.New("x"))-8:])
	if err != nil {
		t.Fatal(err)
	}
	authService := auth.New(pool, auth.Options{SessionLifetime: time.Hour})
	session, err := authService.CreateSessionWithMethod(ctx, "usr_roles_api", "roles-test", "", auth.AuthMethodPassword)
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{Pool: pool, Auth: authService, Workspace: workspaceService}
	handler := New(deps)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: httpserver.SessionCookieName, Value: session.Token})
		req.Header.Set("Hubchat-Workspace-Id", workspaceA.ID)
		if method != http.MethodGet {
			req.Header.Set("Idempotency-Key", "role-"+ids.New("idem"))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	create := request(http.MethodPost, "/v1/roles", `{"key":"billing_specialist","name":"Billing specialist","description":"Billing queue","capabilities":["conversation.read","customer.read"]}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var role roleJSON
	if err := json.NewDecoder(create.Body).Decode(&role); err != nil {
		t.Fatal(err)
	}
	if role.IsBuiltin || role.WorkspaceID != workspaceA.ID {
		t.Fatalf("unexpected role response: %+v", role)
	}

	bad := request(http.MethodPost, "/v1/roles", `{"key":"bad_role","name":"Bad","capabilities":["unknown.capability"]}`)
	if bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid capability status=%d body=%s", bad.Code, bad.Body.String())
	}
	updated := request(http.MethodPatch, "/v1/roles/"+role.ID, `{"name":"Billing lead","capabilities":["conversation.read","conversation.reply"]}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"name":"Billing lead"`) {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}

	// The request helper uses workspace A; switch explicitly to prove the
	// route cannot delete a role by id through another tenant's context.
	req := httptest.NewRequest(http.MethodDelete, "/v1/roles/"+role.ID, nil)
	req.AddCookie(&http.Cookie{Name: httpserver.SessionCookieName, Value: session.Token})
	req.Header.Set("Hubchat-Workspace-Id", workspaceB.ID)
	req.Header.Set("Idempotency-Key", "role-cross-"+ids.New("idem"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace delete status=%d body=%s", response.Code, response.Body.String())
	}

	delete := request(http.MethodDelete, "/v1/roles/"+role.ID, "")
	if delete.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", delete.Code, delete.Body.String())
	}
}
