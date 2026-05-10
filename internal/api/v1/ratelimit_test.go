package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/naufal/latasya-erp/internal/audit"
)

func makeLoginHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	})
}

func wrapWithAuditAndRateLimit(handler http.Handler) http.Handler {
	return audit.RequestContext(LoginRateLimiter()(handler))
}

func loginRequest(ip, username string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.Header.Set("X-Forwarded-For", ip)
	if username != "" {
		r.Form = map[string][]string{"username": {username}}
	}
	return r
}

func TestLoginRateLimiter_AllowsUnderLimit(t *testing.T) {
	h := wrapWithAuditAndRateLimit(makeLoginHandler(http.StatusOK))

	for i := range 5 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, loginRequest("1.2.3.4", "alice"))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestLoginRateLimiter_BlocksOnLimit(t *testing.T) {
	h := wrapWithAuditAndRateLimit(makeLoginHandler(http.StatusUnauthorized))

	for i := range 5 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, loginRequest("10.0.0.1", "bob"))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: expected 401, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, loginRequest("10.0.0.1", "bob"))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request: expected 429, got %d", w.Code)
	}

	var env ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Code != CodeRateLimited {
		t.Errorf("expected code %q, got %q", CodeRateLimited, env.Code)
	}
}

func TestLoginRateLimiter_DifferentIPsIndependent(t *testing.T) {
	h := wrapWithAuditAndRateLimit(makeLoginHandler(http.StatusUnauthorized))

	for range 5 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, loginRequest("192.168.1.1", "carol"))
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, loginRequest("192.168.1.2", "carol"))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("different IP should not be blocked, got %d", w.Code)
	}
}

func TestLoginRateLimiter_SuccessRestoresToken(t *testing.T) {
	fail := makeLoginHandler(http.StatusUnauthorized)
	succeed := makeLoginHandler(http.StatusOK)

	limiter := LoginRateLimiter()
	failH := audit.RequestContext(limiter(fail))
	succeedH := audit.RequestContext(limiter(succeed))

	for range 4 {
		w := httptest.NewRecorder()
		failH.ServeHTTP(w, loginRequest("5.5.5.5", "dave"))
	}

	w := httptest.NewRecorder()
	succeedH.ServeHTTP(w, loginRequest("5.5.5.5", "dave"))
	if w.Code != http.StatusOK {
		t.Fatalf("5th request (success): expected 200, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	failH.ServeHTTP(w, loginRequest("5.5.5.5", "dave"))
	if w.Code == http.StatusTooManyRequests {
		t.Errorf("after success restores token, next fail should still be allowed, got 429")
	}
}

func TestLoginRateLimiter_RetryAfterHeader(t *testing.T) {
	h := wrapWithAuditAndRateLimit(makeLoginHandler(http.StatusUnauthorized))

	for range 5 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, loginRequest("7.7.7.7", "eve"))
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, loginRequest("7.7.7.7", "eve"))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header, got none")
	}
	val, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After %q is not a number: %v", retryAfter, err)
	}
	if val <= 0 {
		t.Errorf("Retry-After should be positive, got %d", val)
	}
}
