package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/naufal/latasya-erp/internal/audit"
)

func TestWriteError_Shape(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "not authenticated", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var env ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if env.Code != CodeUnauthorized {
		t.Errorf("expected code %q, got %q", CodeUnauthorized, env.Code)
	}

	if env.Error != "not authenticated" {
		t.Errorf("expected error %q, got %q", "not authenticated", env.Error)
	}

	if env.Fields != nil {
		t.Errorf("expected Fields to be nil, got %v", env.Fields)
	}
}

func TestWriteError_Fields(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/test", nil)

	fields := map[string]string{
		"contact_id": "required",
		"amount":     "must be positive",
	}

	WriteError(w, r, http.StatusBadRequest, CodeValidationFailed, "validation failed", fields)

	var env ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if env.Fields == nil {
		t.Fatal("expected Fields to be present")
	}

	if env.Fields["contact_id"] != "required" {
		t.Errorf("expected contact_id=required, got %q", env.Fields["contact_id"])
	}

	if env.Fields["amount"] != "must be positive" {
		t.Errorf("expected amount=must be positive, got %q", env.Fields["amount"])
	}
}

func TestWriteError_AllCodes(t *testing.T) {
	codes := []string{
		CodeInvalidRequest,
		CodeValidationFailed,
		CodeUnauthorized,
		CodeForbidden,
		CodeNotFound,
		CodeConflict,
		CodeRateLimited,
		CodeInvalidToken,
		CodePasswordChangeRequired,
		CodeIdempotencyConflict,
		CodeInternal,
	}

	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/test", nil)

			WriteError(w, r, http.StatusBadRequest, code, "test error", nil)

			var env ErrorEnvelope
			if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if env.Code != code {
				t.Errorf("expected code %q, got %q", code, env.Code)
			}

			if env.Error != "test error" {
				t.Errorf("expected error %q, got %q", "test error", env.Error)
			}
		})
	}
}

func TestWriteError_RequestID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test", nil)

	// Use audit.RequestContext middleware to inject request ID
	handler := audit.RequestContext(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusInternalServerError, CodeInternal, "internal error", nil)
	}))

	handler.ServeHTTP(w, r)

	var env ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if env.RequestID == "" {
		t.Errorf("expected non-empty request_id, got empty string")
	}
}
