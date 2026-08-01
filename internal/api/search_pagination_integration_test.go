//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/conversation"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/search"
	"github.com/hubchat/hubchat/internal/workspace"
)

func TestGlobalSearchEndpointUsesCursorEnvelope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	ownerID := ids.New(ids.PrefixUser)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,name,email,password_hash,email_verified_at) VALUES ($1,'Search owner',$2,'x',now())`, ownerID, ownerID+"@example.com"); err != nil {
		t.Fatal(err)
	}
	eventLog := events.New(pool)
	auditLog := audit.New(pool)
	workspaceService := workspace.New(pool, eventLog, auditLog)
	createdWorkspace, err := workspaceService.Bootstrap(ctx, ownerID, "Search workspace", "search-"+ownerID[len(ownerID)-10:])
	if err != nil {
		t.Fatal(err)
	}
	var inboxID string
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes WHERE workspace_id=$1 LIMIT 1`, createdWorkspace.ID).Scan(&inboxID); err != nil {
		t.Fatal(err)
	}
	conversationService := conversation.New(pool, eventLog, auditLog)
	for _, body := range []string{"Refund details one", "Refund details two"} {
		if _, _, err := conversationService.Start(ctx, createdWorkspace.ID, inboxID, "widget", nil, nil, nil, "Visitor", body); err != nil {
			t.Fatal(err)
		}
	}
	customerService := customer.New(pool, eventLog, auditLog, config.Default().Limits)
	customerID := ids.New(ids.PrefixCustomer)
	if _, err := pool.Exec(ctx, `INSERT INTO customers (id,workspace_id,name,email) VALUES ($1,$2,'Refund customer','refund@example.com')`, customerID, createdWorkspace.ID); err != nil {
		t.Fatal(err)
	}
	searchService := search.New(conversationService, customerService)
	deps := Deps{Search: searchService}
	actor := &authorization.Actor{WorkspaceID: createdWorkspace.ID, Role: "owner", MemberID: "mem_search_api"}

	request := func(cursor string) (Page[map[string]any], *httptest.ResponseRecorder) {
		path := "/api/v1/search?q=refund&limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleSearch(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("")
	if firstResponse.Code != http.StatusOK || len(first.Data) != 2 || !first.HasMore || first.NextCursor == nil {
		t.Fatalf("first search page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request(*first.NextCursor)
	if secondResponse.Code != http.StatusOK || len(second.Data) != 1 || second.Data[0]["kind"] != "customer" || second.Data[0]["entity_id"] == first.Data[0]["entity_id"] {
		t.Fatalf("second search page = %d %+v", secondResponse.Code, second)
	}
}
