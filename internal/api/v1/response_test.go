package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON_Success(t *testing.T) {
	w := httptest.NewRecorder()
	body := map[string]string{"key": "val"}

	WriteJSON(w, http.StatusOK, body)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type 'application/json; charset=utf-8', got '%s'", contentType)
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result["key"] != "val" {
		t.Errorf("expected body key='val', got key='%s'", result["key"])
	}
}

func TestWriteJSON_NilBody(t *testing.T) {
	w := httptest.NewRecorder()

	WriteJSON(w, http.StatusNoContent, nil)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("expected Content-Type 'application/json; charset=utf-8', got '%s'", contentType)
	}
}

func TestDecodeJSON_Success(t *testing.T) {
	type TestStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	body := `{"name":"Alice","age":30}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	w := httptest.NewRecorder()

	var result TestStruct
	err := DecodeJSON(w, req, &result)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Name != "Alice" {
		t.Errorf("expected name='Alice', got name='%s'", result.Name)
	}

	if result.Age != 30 {
		t.Errorf("expected age=30, got age=%d", result.Age)
	}
}

func TestDecodeJSON_Oversized(t *testing.T) {
	w := httptest.NewRecorder()

	largeBody := strings.Repeat("x", 2<<20)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(largeBody))

	var result map[string]string
	err := DecodeJSON(w, req, &result)

	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}

	if !strings.Contains(err.Error(), "decode request body") {
		t.Errorf("expected error containing 'decode request body', got: %v", err)
	}
}

func TestDecodeJSON_UnknownField(t *testing.T) {
	type TestStruct struct {
		Known string `json:"known"`
	}

	body := `{"known":"val","unknown_field":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	w := httptest.NewRecorder()

	var result TestStruct
	err := DecodeJSON(w, req, &result)

	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}

	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("expected error containing 'unknown field', got: %v", err)
	}
}

func TestDecodeJSON_Malformed(t *testing.T) {
	body := `{invalid json`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	w := httptest.NewRecorder()

	var result map[string]string
	err := DecodeJSON(w, req, &result)

	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestDecodeJSON_ExactlyAtLimit(t *testing.T) {
	type TestStruct struct {
		Data string `json:"data"`
	}

	body := `{"data":"` + strings.Repeat("x", 1<<20-20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()

	var result TestStruct
	err := DecodeJSON(w, req, &result)

	if err != nil {
		t.Fatalf("expected no error at 1MB limit, got %v", err)
	}
}
