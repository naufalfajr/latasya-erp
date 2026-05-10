package v1_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// counterHandler records how many times it is invoked and returns a JSON
// response. The body of the response embeds the current counter so the test
// can verify replay returns the cached body, not a fresh one.
type counterHandler struct {
	calls  atomic.Int64
	status int
}

func (h *counterHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := h.calls.Add(1)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	status := h.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"call": n, "ok": true})
}

func setupIdempotencyServer(t *testing.T, h http.Handler) (*httptest.Server, string) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "idem-user", "password", "admin")
	sessionID := testutil.CreateTestSession(t, db, userID)

	mw := v1.Idempotency(db)
	chained := audit.RequestContext(auth.RequireAuth(db, mw(h)))
	srv := httptest.NewServer(chained)
	t.Cleanup(srv.Close)
	return srv, sessionID
}

func doPOST(t *testing.T, srv *httptest.Server, sessionID, key string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/test", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

func TestIdempotency_Passthrough_NoHeader(t *testing.T) {
	h := &counterHandler{}
	srv, sid := setupIdempotencyServer(t, h)

	resp := doPOST(t, srv, sid, "", []byte(`{"a":1}`))
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	if got := h.calls.Load(); got != 1 {
		t.Fatalf("calls: got %d want 1", got)
	}

	resp2 := doPOST(t, srv, sid, "", []byte(`{"a":1}`))
	resp2.Body.Close()
	if got := h.calls.Load(); got != 2 {
		t.Fatalf("second call: got %d want 2", got)
	}
}

func TestIdempotency_Passthrough_GET(t *testing.T) {
	h := &counterHandler{}
	srv, sid := setupIdempotencyServer(t, h)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/test", nil)
	req.Header.Set("Idempotency-Key", "k1")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sid})
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/test", nil)
	req2.Header.Set("Idempotency-Key", "k1")
	req2.AddCookie(&http.Cookie{Name: "session_id", Value: sid})
	resp2, err := srv.Client().Do(req2)
	if err != nil {
		t.Fatalf("get2: %v", err)
	}
	resp2.Body.Close()

	if got := h.calls.Load(); got != 2 {
		t.Fatalf("GET should pass through: got %d calls want 2", got)
	}
}

func TestIdempotency_FirstRequest(t *testing.T) {
	h := &counterHandler{}
	srv, sid := setupIdempotencyServer(t, h)

	resp := doPOST(t, srv, sid, "key-first", []byte(`{"a":1}`))
	body := readBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	if got := h.calls.Load(); got != 1 {
		t.Fatalf("calls: got %d want 1", got)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode body: %v body=%s", err, body)
	}
	if parsed["ok"] != true {
		t.Fatalf("body missing ok: %v", parsed)
	}
}

func TestIdempotency_Replay(t *testing.T) {
	h := &counterHandler{}
	srv, sid := setupIdempotencyServer(t, h)

	resp1 := doPOST(t, srv, sid, "key-replay", []byte(`{"a":1}`))
	body1 := readBody(t, resp1)

	resp2 := doPOST(t, srv, sid, "key-replay", []byte(`{"a":1}`))
	body2 := readBody(t, resp2)

	if got := h.calls.Load(); got != 1 {
		t.Fatalf("handler should run once: got %d", got)
	}
	if resp1.StatusCode != resp2.StatusCode {
		t.Fatalf("status mismatch: %d vs %d", resp1.StatusCode, resp2.StatusCode)
	}
	if !bytes.Equal(body1, body2) {
		t.Fatalf("body mismatch:\n  first: %s\n second: %s", body1, body2)
	}
}

func TestIdempotency_HashMismatch(t *testing.T) {
	h := &counterHandler{}
	srv, sid := setupIdempotencyServer(t, h)

	resp1 := doPOST(t, srv, sid, "key-mismatch", []byte(`{"a":1}`))
	resp1.Body.Close()

	resp2 := doPOST(t, srv, sid, "key-mismatch", []byte(`{"a":2}`))
	body2 := readBody(t, resp2)

	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d want 409", resp2.StatusCode)
	}
	var env v1.ErrorEnvelope
	if err := json.Unmarshal(body2, &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, body2)
	}
	if env.Code != v1.CodeIdempotencyConflict {
		t.Fatalf("code: got %q want %q", env.Code, v1.CodeIdempotencyConflict)
	}
	if got := h.calls.Load(); got != 1 {
		t.Fatalf("conflict path should not invoke handler: got %d calls", got)
	}
}

func TestIdempotency_FailureNotCached(t *testing.T) {
	h := &counterHandler{status: http.StatusInternalServerError}
	srv, sid := setupIdempotencyServer(t, h)

	resp1 := doPOST(t, srv, sid, "key-fail", []byte(`{"a":1}`))
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusInternalServerError {
		t.Fatalf("first status: got %d want 500", resp1.StatusCode)
	}

	resp2 := doPOST(t, srv, sid, "key-fail", []byte(`{"a":1}`))
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Fatalf("second status: got %d want 500", resp2.StatusCode)
	}
	if got := h.calls.Load(); got != 2 {
		t.Fatalf("failures must not be cached: got %d calls want 2", got)
	}
}

func TestIdempotency_Concurrent(t *testing.T) {
	h := &counterHandler{}
	srv, sid := setupIdempotencyServer(t, h)

	const N = 10
	var wg sync.WaitGroup
	bodies := make([][]byte, N)
	statuses := make([]int, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := doPOST(t, srv, sid, "key-concurrent", []byte(`{"a":1}`))
			statuses[i] = resp.StatusCode
			bodies[i] = readBody(t, resp)
		}(i)
	}
	wg.Wait()

	if got := h.calls.Load(); got != 1 {
		t.Fatalf("handler must run exactly once across concurrent calls: got %d", got)
	}
	for i := 1; i < N; i++ {
		if statuses[i] != statuses[0] {
			t.Fatalf("status[%d]=%d differs from status[0]=%d", i, statuses[i], statuses[0])
		}
		if !bytes.Equal(bodies[i], bodies[0]) {
			t.Fatalf("body[%d] differs from body[0]:\n  [0]: %s\n  [%d]: %s", i, bodies[0], i, bodies[i])
		}
	}
}
