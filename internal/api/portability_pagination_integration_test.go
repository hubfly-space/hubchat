//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"github.com/hubchat/hubchat/internal/portability"
)

func TestPortabilityHistoriesUseWorkspaceCursors(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `
		INSERT INTO workspaces (id,name,slug) VALUES
			('wrk_port_a','Portability A','port-a'),
			('wrk_port_b','Portability B','port-b');
		INSERT INTO export_requests (id,workspace_id,kind,format,state,created_at) VALUES
			('exp_a1','wrk_port_a','workspace','json','completed','2026-07-31T12:00:00Z'),
			('exp_a2','wrk_port_a','workspace','json','failed','2026-07-30T12:00:00Z'),
			('exp_b1','wrk_port_b','workspace','json','completed','2026-07-31T13:00:00Z');
		INSERT INTO import_requests (id,workspace_id,kind,state,created_at) VALUES
			('imp_a1','wrk_port_a','workspace','completed','2026-07-31T12:00:00Z'),
			('imp_a2','wrk_port_a','workspace','failed','2026-07-30T12:00:00Z'),
			('imp_b1','wrk_port_b','workspace','completed','2026-07-31T13:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}

	deps := Deps{Portability: portability.New(pool, nil, nil)}
	actor := &authorization.Actor{WorkspaceID: "wrk_port_a", Role: "owner"}
	requestExports := func(path string) (Page[portability.Request], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListExports(deps)(response, req)
		var page Page[portability.Request]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}
	requestImports := func(path string) (Page[portability.Request], *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(authorization.WithActor(ctx, actor))
		response := httptest.NewRecorder()
		handleListImports(deps)(response, req)
		var page Page[portability.Request]
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		return page, response
	}

	exports, exportResponse := requestExports("/api/v1/portability/exports?limit=1")
	if exportResponse.Code != http.StatusOK || !exports.HasMore || exports.NextCursor == nil || len(exports.Data) != 1 || exports.Data[0].ID != "exp_a1" {
		t.Fatalf("first export page = %d %+v", exportResponse.Code, exports)
	}
	imports, importResponse := requestImports("/api/v1/portability/imports?limit=1")
	if importResponse.Code != http.StatusOK || !imports.HasMore || imports.NextCursor == nil || len(imports.Data) != 1 || imports.Data[0].ID != "imp_a1" {
		t.Fatalf("first import page = %d %+v", importResponse.Code, imports)
	}

	secondExports, _ := requestExports("/api/v1/portability/exports?limit=1&cursor=" + *exports.NextCursor)
	secondImports, _ := requestImports("/api/v1/portability/imports?limit=1&cursor=" + *imports.NextCursor)
	if len(secondExports.Data) != 1 || secondExports.Data[0].ID != "exp_a2" || secondExports.HasMore {
		t.Fatalf("second export page = %+v", secondExports)
	}
	if len(secondImports.Data) != 1 || secondImports.Data[0].ID != "imp_a2" || secondImports.HasMore {
		t.Fatalf("second import page = %+v", secondImports)
	}
}
