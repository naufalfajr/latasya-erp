package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// Identifies the caller the way the limiter does: CF-Connecting-IP, not the
// forgeable X-Forwarded-For.
func loginRequest(ip, username string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.Header.Set("CF-Connecting-IP", ip)
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

// Identifies the caller the way the limiter does: CF-Connecting-IP, not the
// forgeable X-Forwarded-For.
func portalRequest(ip, path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("CF-Connecting-IP", ip)
	return r
}

// What keeps the short portal code from being swept by brute force.
func TestPortalCodeLimiter_BlocksAfterMisses(t *testing.T) {
	h := audit.RequestContext(PortalCodeLimiter()(makeLoginHandler(http.StatusNotFound)))

	for i := range portalCodeBucketSize {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, portalRequest("5.6.7.8", "/p/zzzz-000"))
		if w.Code != http.StatusNotFound {
			t.Fatalf("miss %d: expected 404 to pass through, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, portalRequest("5.6.7.8", "/p/zzzz-000"))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after %d misses, got %d", portalCodeBucketSize, w.Code)
	}
	if ra := w.Header().Get("Retry-After"); ra != strconv.Itoa(int(portalCodeWindow.Seconds())) {
		t.Errorf("expected Retry-After %d, got %q", int(portalCodeWindow.Seconds()), ra)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("parents get an HTML page, not the JSON API: expected a text/plain 429 body, got %q", ct)
	}
}

// A parent refreshing their own page must never be throttled.
func TestPortalCodeLimiter_SuccessDoesNotConsumeQuota(t *testing.T) {
	h := audit.RequestContext(PortalCodeLimiter()(makeLoginHandler(http.StatusOK)))

	for i := range portalCodeBucketSize * 3 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, portalRequest("9.9.9.9", "/p/andi-829"))
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: a valid code should never be throttled, got %d", i+1, w.Code)
		}
	}
}

func TestPortalCodeLimiter_DifferentIPsIndependent(t *testing.T) {
	h := audit.RequestContext(PortalCodeLimiter()(makeLoginHandler(http.StatusNotFound)))

	for range portalCodeBucketSize + 1 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, portalRequest("1.1.1.1", "/p/zzzz-000"))
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, portalRequest("2.2.2.2", "/p/zzzz-000"))
	if w.Code != http.StatusNotFound {
		t.Errorf("a second IP has its own bucket: expected 404, got %d", w.Code)
	}
}

// Regression: keying on the forgeable X-Forwarded-For let one caller spend a
// fresh guess budget per request, making the code space sweepable in minutes.
func TestPortalCodeLimiter_XFFCannotMintFreshBuckets(t *testing.T) {
	h := audit.RequestContext(PortalCodeLimiter()(makeLoginHandler(http.StatusNotFound)))

	blocked := 0
	for i := range portalCodeBucketSize * 3 {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/p/zzzz-000", nil)
		r.RemoteAddr = "198.51.100.9:1234" // one real socket throughout
		r.Header.Set("X-Forwarded-For", strconv.Itoa(i)+".0.0.1, 198.51.100.9")
		h.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatal("varying X-Forwarded-For bypassed the limiter entirely: the portal code is brute-forceable")
	}
}

// Behind Cloudflare the socket is Cloudflare's, so without CF-Connecting-IP
// every parent would share one bucket.
func TestPortalCodeLimiter_CFConnectingIPSeparatesClients(t *testing.T) {
	h := audit.RequestContext(PortalCodeLimiter()(makeLoginHandler(http.StatusNotFound)))

	drain := func(cfip string) {
		for range portalCodeBucketSize + 1 {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/p/zzzz-000", nil)
			r.RemoteAddr = "172.16.0.1:443" // same Cloudflare edge socket
			r.Header.Set("CF-Connecting-IP", cfip)
			h.ServeHTTP(w, r)
		}
	}
	drain("203.0.113.7")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/p/zzzz-000", nil)
	r.RemoteAddr = "172.16.0.1:443"
	r.Header.Set("CF-Connecting-IP", "203.0.113.8")
	h.ServeHTTP(w, r)
	if w.Code == http.StatusTooManyRequests {
		t.Error("a second real client behind the same Cloudflare edge must have its own bucket")
	}
}

// Regression: keyed on X-Forwarded-For, varying one header gave unlimited
// password guesses against a known username.
func TestLoginRateLimiter_XFFCannotMintFreshBuckets(t *testing.T) {
	h := wrapWithAuditAndRateLimit(makeLoginHandler(http.StatusUnauthorized))

	blocked := 0
	for i := range rateLimitBucketSize * 4 {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		r.RemoteAddr = "198.51.100.9:1234" // one real socket throughout
		r.Header.Set("X-Forwarded-For", strconv.Itoa(i)+".0.0.1, 198.51.100.9")
		r.Form = map[string][]string{"username": {"admin"}}
		h.ServeHTTP(w, r)
		if w.Code == http.StatusTooManyRequests {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatal("varying X-Forwarded-For bypassed the login limiter: passwords are brute-forceable")
	}
}
