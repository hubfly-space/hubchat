//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/config"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database/dbtest"
)

func TestCustomerAndCompanyExportsAreWorkspaceScopedCSV(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id, name, slug) VALUES
			('wrk_export_a', 'Export A', 'export-a'),
			('wrk_export_b', 'Export B', 'export-b');
		INSERT INTO customers (id, workspace_id, name, email, verification) VALUES
			('cus_export_a', 'wrk_export_a', 'Alice Export', 'alice@example.com', 'verified'),
			('cus_export_b', 'wrk_export_b', 'Bob Other', 'bob@example.com', 'verified');
		INSERT INTO companies (id, workspace_id, name, domain) VALUES
			('com_export_a', 'wrk_export_a', 'Acme Export', 'acme.example'),
			('com_export_a2', 'wrk_export_a', 'Zeta Export', 'zeta.example'),
			('com_export_b', 'wrk_export_b', 'Other Export', 'other.example')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Customer: customer.New(pool, nil, nil, config.Limits{})}
	actor := &authorization.Actor{WorkspaceID: "wrk_export_a", Role: "owner"}

	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req = req.WithContext(authorization.WithActor(req.Context(), actor))
		response := httptest.NewRecorder()
		if strings.HasPrefix(path, "/api/v1/customers/export") {
			handleExportCustomers(deps)(response, req)
		} else {
			handleExportCompanies(deps)(response, req)
		}
		return response
	}

	customers := request("/api/v1/customers/export")
	if customers.Code != http.StatusOK || !strings.Contains(customers.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("customer export response = %d %q", customers.Code, customers.Body.String())
	}
	if !strings.Contains(customers.Body.String(), "Alice Export") || strings.Contains(customers.Body.String(), "Bob Other") {
		t.Fatalf("customer export crossed workspace boundary: %s", customers.Body.String())
	}

	companies := request("/api/v1/companies/export")
	if companies.Code != http.StatusOK || !strings.Contains(companies.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("company export response = %d %q", companies.Code, companies.Body.String())
	}
	if !strings.Contains(companies.Body.String(), "Acme Export") || strings.Contains(companies.Body.String(), "Other Export") {
		t.Fatalf("company export crossed workspace boundary: %s", companies.Body.String())
	}

	firstRequest := httptest.NewRequest(http.MethodGet, "/api/v1/companies?limit=1", nil)
	firstRequest = firstRequest.WithContext(authorization.WithActor(firstRequest.Context(), actor))
	firstResponse := httptest.NewRecorder()
	handleListCompanies(deps)(firstResponse, firstRequest)
	var firstPage Page[map[string]any]
	if err := json.NewDecoder(firstResponse.Body).Decode(&firstPage); err != nil {
		t.Fatal(err)
	}
	if firstResponse.Code != http.StatusOK || !firstPage.HasMore || firstPage.NextCursor == nil {
		t.Fatalf("company pagination first page = %d %+v", firstResponse.Code, firstPage)
	}
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/companies?limit=1&cursor="+*firstPage.NextCursor, nil)
	secondRequest = secondRequest.WithContext(authorization.WithActor(secondRequest.Context(), actor))
	secondResponse := httptest.NewRecorder()
	handleListCompanies(deps)(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusOK || strings.Contains(secondResponse.Body.String(), "Acme Export") {
		t.Fatalf("company pagination repeated the first page: %d %s", secondResponse.Code, secondResponse.Body.String())
	}
}
