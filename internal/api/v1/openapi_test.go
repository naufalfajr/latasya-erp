package v1_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
)

func TestServeOpenAPI(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil)
	rec := httptest.NewRecorder()

	v1.ServeOpenAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/yaml") {
		t.Errorf("Content-Type: got %q, want application/yaml prefix", ct)
	}
	if !strings.Contains(rec.Body.String(), "openapi:") {
		t.Error("expected body to contain \"openapi:\"")
	}
}
