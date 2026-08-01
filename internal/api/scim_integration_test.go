//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hubchat/hubchat/internal/apikey"
	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestSCIMRoutesRequireScopedWorkspaceKeyAndSupportLifecycle(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	workspaceService := workspace.New(pool, nil, nil)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, name, email, password_hash, email_verified_at) VALUES ($1, 'SCIM Owner', $2, 'unused', now())`, "usr_scim_api", "scim-api@example.com"); err != nil {
		t.Fatal(err)
	}
	ws, err := workspaceService.Bootstrap(ctx, "usr_scim_api", "SCIM API", "scim-api-"+ids.New("x")[len(ids.New("x"))-8:])
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	owner, err := workspaceService.ActorForUser(ctx, ws.ID, "usr_scim_api")
	if err != nil {
		t.Fatalf("owner actor: %v", err)
	}
	keys := apikey.New(pool)
	created, err := keys.Create(ctx, ws.ID, owner.MemberID, "SCIM", []string{"member.manage"}, nil)
	if err != nil {
		t.Fatalf("create SCIM key: %v", err)
	}
	deps := Deps{Pool: pool, Auth: auth.New(pool, auth.Options{SessionLifetime: time.Hour}), Workspace: workspaceService, APIKeys: keys}
	handler := New(deps)

	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	if response := request(http.MethodGet, "/v1/scim/v2.0/"+ws.ID+"/Users", "", created.Token); response.Code != http.StatusOK {
		t.Fatalf("initial SCIM list status=%d body=%s", response.Code, response.Body.String())
	}
	createResponse := request(http.MethodPost, "/v1/scim/v2.0/"+ws.ID+"/Users", `{"externalId":"directory-api-1","userName":"api-agent@example.com","displayName":"API Agent","active":true}`, created.Token)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("SCIM create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var resource struct {
		ID     string `json:"id"`
		Active bool   `json:"active"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&resource); err != nil {
		t.Fatal(err)
	}
	if resource.ID == "" || !resource.Active {
		t.Fatalf("unexpected SCIM resource: %+v", resource)
	}
	memberKey, err := keys.Create(ctx, ws.ID, resource.ID, "deprovisioned member key", []string{"member.read"}, nil)
	if err != nil {
		t.Fatalf("create member key: %v", err)
	}

	deleteResponse := request(http.MethodDelete, "/v1/scim/v2.0/"+ws.ID+"/Users/"+resource.ID, "", created.Token)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("SCIM delete status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := keys.Authenticate(ctx, memberKey.Token); err == nil {
		t.Fatal("deprovisioned member API key still authenticated")
	}
	getResponse := request(http.MethodGet, "/v1/scim/v2.0/"+ws.ID+"/Users/"+resource.ID, "", created.Token)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"active":false`) {
		t.Fatalf("SCIM inactive resource status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}

	other, err := workspaceService.Bootstrap(ctx, "usr_scim_api", "Other SCIM API", "scim-other-"+ids.New("x")[len(ids.New("x"))-8:])
	if err != nil {
		t.Fatalf("other workspace: %v", err)
	}
	if response := request(http.MethodGet, "/v1/scim/v2.0/"+other.ID+"/Users", "", created.Token); response.Code != http.StatusForbidden {
		t.Fatalf("cross-workspace SCIM status=%d body=%s", response.Code, response.Body.String())
	}

	readOnly, err := keys.Create(ctx, ws.ID, owner.MemberID, "SCIM read only", []string{"member.read"}, nil)
	if err != nil {
		t.Fatalf("create read-only key: %v", err)
	}
	if response := request(http.MethodGet, "/v1/scim/v2.0/"+ws.ID+"/Users", "", readOnly.Token); response.Code != http.StatusForbidden {
		t.Fatalf("unscoped SCIM status=%d body=%s", response.Code, response.Body.String())
	}
}
