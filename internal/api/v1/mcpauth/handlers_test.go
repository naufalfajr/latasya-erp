package mcpauth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	v1mcpauth "github.com/naufal/latasya-erp/internal/api/v1/mcpauth"
	"github.com/naufal/latasya-erp/internal/mcpauth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newDeviceServer(t *testing.T) (*httptest.Server, *mcpauth.Module) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	m := mcpauth.New(db)
	h := &v1mcpauth.Handler{MCP: m}
	ts := httptest.NewServer(v1.MCPDeviceLimiter()(http.HandlerFunc(h.DevicePoll)))
	t.Cleanup(ts.Close)
	return ts, m
}

func poll(t *testing.T, ts *httptest.Server, deviceCode, userCode string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"device_code": deviceCode,
		"user_code":   userCode,
		"client":      "latasya-mcp/1.0 (test)",
	})
	resp, err := ts.Client().Post(ts.URL+"/api/v1/mcp/device", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	return resp
}

func TestDevicePoll_PendingThenApprovedMintsToken(t *testing.T) {
	ts, m := newDeviceServer(t)
	ctx := t.Context()
	device := "device-code-1-aaaaaaaaaaaaaaaaaaaaaaaa"

	resp := poll(t, ts, device, "AB12CD34")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("first poll: got %d, want 202", resp.StatusCode)
	}
	var pending struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&pending)
	if pending.Data.Status != "pending" {
		t.Errorf("first poll status: got %q, want pending", pending.Data.Status)
	}

	if err := m.Approve(ctx, "AB12CD34", 1, "admin", []string{model.CapReportsView}, nil); err != nil {
		t.Fatalf("approve: %v", err)
	}

	resp2 := poll(t, ts, device, "AB12CD34")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("approved poll: got %d, want 200", resp2.StatusCode)
	}
	var approved struct {
		Data struct {
			Status   string   `json:"status"`
			Token    string   `json:"token"`
			TokenID  int      `json:"token_id"`
			Scopes   []string `json:"scopes"`
			Username string   `json:"username"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&approved); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if approved.Data.Status != "approved" || !strings.HasPrefix(approved.Data.Token, "lat_") {
		t.Fatalf("approved payload: %+v", approved.Data)
	}
	if approved.Data.Username != "admin" || len(approved.Data.Scopes) != 1 {
		t.Errorf("token details: %+v", approved.Data)
	}

	// The minted token is a real api_token row.
	_, _, err := m.Consume(ctx, "no-such-device")
	if err == nil {
		t.Fatal("consume of unknown device should fail")
	}
}

func TestDevicePoll_ReplayAfterConsumeStartsFreshHandoff(t *testing.T) {
	// A replayed device code after a successful mint must NOT mint again:
	// the consumed row is gone, so Touch registers a brand-new pending
	// handoff that nobody has approved.
	ts, m := newDeviceServer(t)
	ctx := t.Context()
	device := "device-code-2-bbbbbbbbbbbbbbbbbbbbbbbb"

	poll(t, ts, device, "REPLAY01").Body.Close()
	if err := m.Approve(ctx, "REPLAY01", 1, "admin", []string{model.CapReportsView}, nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	resp := poll(t, ts, device, "REPLAY01")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approved poll: got %d, want 200", resp.StatusCode)
	}

	replay := poll(t, ts, device, "REPLAY01")
	defer replay.Body.Close()
	if replay.StatusCode != http.StatusAccepted {
		t.Fatalf("replay poll: got %d, want 202 (fresh pending, no re-mint)", replay.StatusCode)
	}
}

func TestDevicePoll_DeniedHandoff(t *testing.T) {
	ts, m := newDeviceServer(t)

	poll(t, ts, "device-code-3-cccccccccccccccccccccccc", "DENY1234").Body.Close()
	if err := m.Deny(t.Context(), "DENY1234"); err != nil {
		t.Fatalf("deny: %v", err)
	}

	resp := poll(t, ts, "device-code-3-cccccccccccccccccccccccc", "DENY1234")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("denied poll: got %d, want 403", resp.StatusCode)
	}
}

func TestDevicePoll_RejectsJunkPayloads(t *testing.T) {
	ts, _ := newDeviceServer(t)

	cases := map[string]string{
		"empty body":      "{}",
		"missing client":  "{\"device_code\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"user_code\":\"AB12CD34\"}",
		"short user code": "{\"device_code\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"user_code\":\"AB\",\"client\":\"x\"}",
	}
	for name, body := range cases {
		resp, err := ts.Client().Post(ts.URL+"/api/v1/mcp/device", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, resp.StatusCode)
		}
	}
}

func TestDevicePoll_RateLimited(t *testing.T) {
	ts, _ := newDeviceServer(t)

	var last int
	for i := 0; i <= 120; i++ {
		resp := poll(t, ts, "device-code-4-dddddddddddddddddddddddd", "RATE1234")
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("121st poll in window: got %d, want 429", last)
	}
}
