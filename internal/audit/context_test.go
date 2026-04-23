package audit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/naufal/latasya-erp/internal/audit"
)

func TestRequestContext_AttachesRequestIDAndIP(t *testing.T) {
	var gotRequestID, gotIP string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID = audit.RequestIDFromContext(r.Context())
		gotIP = audit.ClientIPFromContext(r.Context())
	})

	srv := httptest.NewServer(audit.RequestContext(next))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(gotRequestID) != 32 { // 16 bytes hex-encoded
		t.Errorf("request_id length = %d, want 32", len(gotRequestID))
	}
	if gotIP == "" {
		t.Errorf("client IP should be populated, got empty")
	}
}

func TestRequestContext_XForwardedFor(t *testing.T) {
	var gotIP string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP = audit.ClientIPFromContext(r.Context())
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	rr := httptest.NewRecorder()
	audit.RequestContext(next).ServeHTTP(rr, req)

	if gotIP != "203.0.113.5" {
		t.Errorf("X-Forwarded-For first hop = %q, want 203.0.113.5", gotIP)
	}
}

func TestRequestContext_CFConnectingIP(t *testing.T) {
	var gotIP string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP = audit.ClientIPFromContext(r.Context())
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("CF-Connecting-IP", "198.51.100.7")
	rr := httptest.NewRecorder()
	audit.RequestContext(next).ServeHTTP(rr, req)

	if gotIP != "198.51.100.7" {
		t.Errorf("CF-Connecting-IP = %q, want 198.51.100.7", gotIP)
	}
}

func TestRequestContext_DifferentRequestsGetDifferentIDs(t *testing.T) {
	seen := make(map[string]bool)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := audit.RequestIDFromContext(r.Context())
		seen[id] = true
	})

	srv := httptest.NewServer(audit.RequestContext(next))
	defer srv.Close()

	for i := 0; i < 5; i++ {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if len(seen) != 5 {
		t.Errorf("expected 5 distinct request IDs, got %d", len(seen))
	}
}
