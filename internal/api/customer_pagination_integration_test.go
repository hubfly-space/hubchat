//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestCustomerSearchUsesWorkspaceCursor(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_customer_page_a','Customers A','customers-page-a'),
			('wrk_customer_page_b','Customers B','customers-page-b');
		INSERT INTO customers (id,workspace_id,name,email,verification,last_seen_at) VALUES
			('cus_customer_page_a1','wrk_customer_page_a','Alice New','alice-new@example.com','verified','2026-07-31T12:00:00Z'),
			('cus_customer_page_a2','wrk_customer_page_a','Alice Old','alice-old@example.com','verified','2026-07-30T12:00:00Z'),
			('cus_customer_page_b1','wrk_customer_page_b','Alice Other','alice-other@example.com','verified','2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Customer: customer.New(pool, nil, nil, config.Limits{})}
	actor := &authorization.Actor{WorkspaceID: "wrk_customer_page_a", Role: "owner"}
	request := func(path string) (Page[map[string]any], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleSearchCustomers(deps)(response, req)
		var page Page[map[string]any]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	first, firstResponse := request("/api/v1/customers?q=Alice&limit=1")
	if firstResponse.Code != http.StatusOK || !first.HasMore || first.NextCursor == nil || len(first.Data) != 1 || first.Data[0]["id"] != "cus_customer_page_a1" {
		t.Fatalf("first customer page = %d %+v", firstResponse.Code, first)
	}
	second, secondResponse := request("/api/v1/customers?q=Alice&limit=1&cursor=" + *first.NextCursor)
	if secondResponse.Code != http.StatusOK || second.HasMore || len(second.Data) != 1 || second.Data[0]["id"] != "cus_customer_page_a2" {
		t.Fatalf("second customer page = %d %+v", secondResponse.Code, second)
	}
	if second.Data[0]["id"] == "cus_customer_page_b1" {
		t.Fatal("customer search crossed the workspace boundary")
	}
}
