package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIEndpointServesEmbeddedContract(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/openapi.json", nil)
	res := httptest.NewRecorder()
	New(Deps{}).ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	var document struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("openapi version = %q", document.OpenAPI)
	}
	if len(document.Paths) < 40 {
		t.Fatalf("paths = %d, want at least 40", len(document.Paths))
	}
}
