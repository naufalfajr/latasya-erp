package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerRateLimiter_CapsBurstWithoutRefund(t *testing.T) {
	// Every request consumes quota, so exactly bearerRateLimitBucket calls
	// succeed and the next one is throttled — even though all are 2xx.
	h := BearerRateLimiter()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var last int
	for i := 1; i <= bearerRateLimitBucket+2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil))
		last = w.Code
		if i <= bearerRateLimitBucket && last != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i, last)
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("request beyond bucket: got %d, want 429", last)
	}
}

func TestBearerRateLimiter_EnvelopeAndRetryAfter(t *testing.T) {
	h := BearerRateLimiter()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for range bearerRateLimitBucket {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil))
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra == "" {
		t.Error("Retry-After header missing")
	}
	var env struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != CodeRateLimited {
		t.Errorf("error code: got %q, want %q", env.Code, CodeRateLimited)
	}
}

func TestBearerRateLimiter_SeparateCredentialsSeparateBuckets(t *testing.T) {
	// Keyed by credential, not IP: exhausting one token's bucket must not
	// throttle a different token behind the same address.
	limited := BearerRateLimiter()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	asToken := func(id int) {
		t.Helper()
		w := httptest.NewRecorder()
		limited.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil).
			WithContext(context.WithValue(context.Background(), tokenIDKey, id)))
		if w.Code != http.StatusOK {
			t.Fatalf("token %d: got %d, want 200", id, w.Code)
		}
	}

	for range bearerRateLimitBucket {
		asToken(1)
	}

	// Same token: throttled now.
	w := httptest.NewRecorder()
	limited.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/invoices", nil).
		WithContext(context.WithValue(context.Background(), tokenIDKey, 1)))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted token: got %d, want 429", w.Code)
	}

	// Different token: its own bucket, still allowed.
	asToken(2)
}

func TestBearerRateLimiter_LoginLimiterStillRefunds(t *testing.T) {
	// Regression guard for the refundOn2xx split: the login limiter must keep
	// refunding successful attempts so only failures consume quota.
	h := LoginRateLimiter()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := range 50 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, loginRequest("1.2.3.4", "alice"))
		if w.Code != http.StatusOK {
			t.Fatalf("successful login attempt %d throttled; refundOn2xx broken", i)
		}
	}
}
