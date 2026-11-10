package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders_SetOnEveryResponse(t *testing.T) {
	h := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/invoices", nil))

	for name, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "same-origin",
		"X-Frame-Options":        "DENY",
	} {
		if got := w.Header().Get(name); got != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy missing")
	}
	for _, directive := range []string{"default-src 'self'", "script-src 'self' 'unsafe-inline' 'unsafe-eval'", "connect-src 'self'", "frame-ancestors 'none'", "form-action 'self'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP missing %q in %q", directive, csp)
		}
	}
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must be absent in dev mode, got %q", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Errorf("HTML page must not get Cache-Control from the middleware, got %q", got)
	}
}

func TestSecurityHeaders_HSTSOnlyInProduction(t *testing.T) {
	h := SecurityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/", nil))
	if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Errorf("HSTS in production mode: got %q", got)
	}
}

func TestSecurityHeaders_APIResponsesNotStored(t *testing.T) {
	h := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for _, path := range []string{"/api/v1/invoices", "/api/v1/reports/trial-balance?from=2026-01-01"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control: got %q, want no-store", path, got)
		}
	}

	// Non-API paths stay cacheable.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil))
	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Errorf("static asset must not get no-store, got %q", got)
	}
}

func TestSecurityHeaders_NeverOverridesInnerHandler(t *testing.T) {
	h := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/p/AB2C-D3F4", nil))
	if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("portal Cache-Control overridden: got %q", got)
	}
}
