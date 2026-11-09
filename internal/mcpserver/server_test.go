package mcpserver_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/account"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	v1accounts "github.com/naufal/latasya-erp/internal/api/v1/accounts"
	v1auth "github.com/naufal/latasya-erp/internal/api/v1/auth"
	v1income "github.com/naufal/latasya-erp/internal/api/v1/income"
	"github.com/naufal/latasya-erp/internal/idempotency"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/mcpserver"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// newStack builds the production /mcp chain: apiMux (with real domain
// handlers and idempotency) behind BearerOrCookie + BearerRateLimiter, then
// the MCP server replaying into it.
func newStack(t *testing.T, scopes []string) (*httptest.Server, *sql.DB, string) {
	t.Helper()
	db := testutil.SetupTestDB(t)

	idem := v1.Idempotency(idempotency.New(db))
	apiMux := http.NewServeMux()
	v1auth.New(db, true).RegisterRoutes(apiMux)
	(&v1accounts.Handler{Accounts: account.New(db)}).RegisterRoutes(apiMux)
	(&v1income.Handler{Journals: journal.New(db)}).RegisterRoutes(apiMux, idem)

	srv := &mcpserver.Server{API: apiMux}
	mux := http.NewServeMux()
	mux.Handle("POST /mcp", v1.BearerOrCookie(db)(v1.BearerRateLimiter()(srv)))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	_, token, err := testutil.CreateAPIToken(db, adminID, "mcp-remote-test", scopes, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ts, db, token
}

func rpc(t *testing.T, ts *httptest.Server, token, method string, params any, id int) (int, string) {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.URL+"/mcp", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("rpc: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 1<<20)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, string(buf[:n])
}

func TestMCP_UnauthorizedCarriesDiscoveryHeader(t *testing.T) {
	ts, _, _ := newStack(t, nil)
	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", resp.StatusCode)
	}
	if www := resp.Header.Get("WWW-Authenticate"); !strings.Contains(www, "oauth-protected-resource") {
		t.Fatalf("WWW-Authenticate must advertise RFC 9728 discovery, got %q", www)
	}
}

func TestMCP_InitializeAndMethodGuards(t *testing.T) {
	ts, _, token := newStack(t, []string{model.CapAccountsManage})

	status, body := rpc(t, ts, token, "initialize", map[string]string{"protocolVersion": "2025-06-18"}, 1)
	if status != 200 || !strings.Contains(body, `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize: %d %s", status, body)
	}

	// Unknown method → -32601.
	_, body = rpc(t, ts, token, "bogus/method", nil, 2)
	if !strings.Contains(body, "-32601") {
		t.Fatalf("unknown method: %s", body)
	}

	// GET → 405; wrong content type → 415; foreign Origin → 403.
	req, _ := http.NewRequest("GET", ts.URL+"/mcp", nil)
	resp, _ := ts.Client().Do(req)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: got %d", resp.StatusCode)
	}
	resp.Body.Close()

	req2, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`{}`))
	req2.Header.Set("Content-Type", "text/plain")
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := ts.Client().Do(req2)
	if resp2.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("content type: got %d", resp2.StatusCode)
	}
	resp2.Body.Close()

	req3, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"ping"}`))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+token)
	req3.Header.Set("Origin", "https://evil.example")
	resp3, _ := ts.Client().Do(req3)
	if resp3.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin: got %d", resp3.StatusCode)
	}
	resp3.Body.Close()
}

func TestMCP_ToolsListScopedByCapabilities(t *testing.T) {
	ts, _, token := newStack(t, []string{model.CapReportsView, model.CapAccountsManage})

	_, body := rpc(t, ts, token, "tools/list", nil, 1)
	if !strings.Contains(body, `"createAccount"`) {
		t.Error("accounts.manage token must see createAccount")
	}
	if strings.Contains(body, `"createIncome"`) {
		t.Error("token without income.manage must not see createIncome")
	}
	if strings.Contains(body, `"getInvoicePDF"`) {
		t.Error("token without invoices.manage must not see getInvoicePDF")
	}
}

func TestMCP_ToolsCall_CapabilityHidingHasNoOracle(t *testing.T) {
	ts, _, token := newStack(t, []string{model.CapReportsView})

	// A real tool the caller lacks and a nonexistent tool must be
	// indistinguishable.
	_, hidden := rpc(t, ts, token, "tools/call", map[string]any{"name": "createAccount", "arguments": map[string]any{}}, 1)
	_, ghost := rpc(t, ts, token, "tools/call", map[string]any{"name": "doesNotExist", "arguments": map[string]any{}}, 2)
	if !strings.Contains(hidden, "unknown tool") || !strings.Contains(ghost, "unknown tool") {
		t.Fatalf("capability oracle:\nhidden=%s\nghost=%s", hidden, ghost)
	}
}

func TestMCP_ToolsCall_ReplaysThroughAPIWithIdempotency(t *testing.T) {
	ts, db, token := newStack(t, []string{model.CapIncomeManage})

	var cashID, revenueID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE account_type='asset' LIMIT 1").Scan(&cashID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT id FROM accounts WHERE account_type='revenue' LIMIT 1").Scan(&revenueID); err != nil {
		t.Fatal(err)
	}

	args := map[string]any{"body": map[string]any{
		"entry_date": "2026-11-09", "description": "remote mcp income",
		"amount": "50000", "revenue_account": revenueID, "deposit_account": cashID,
	}}
	_, first := rpc(t, ts, token, "tools/call", map[string]any{"name": "createIncome", "arguments": args}, 1)
	if strings.Contains(first, "isError") {
		t.Fatalf("createIncome failed: %s", first)
	}
	_, retry := rpc(t, ts, token, "tools/call", map[string]any{"name": "createIncome", "arguments": args}, 2)

	// Same deterministic key ⇒ same replayed response body.
	var firstResp, retryResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	json.Unmarshal([]byte(first), &firstResp)
	json.Unmarshal([]byte(retry), &retryResp)
	if len(firstResp.Result.Content) == 0 || len(retryResp.Result.Content) == 0 ||
		firstResp.Result.Content[0].Text != retryResp.Result.Content[0].Text {
		t.Fatalf("identical call did not replay:\nfirst=%s\nretry=%s", first, retry)
	}
}

func TestMCP_ParseErrors(t *testing.T) {
	ts, _, token := newStack(t, nil)

	req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader("this is not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := ts.Client().Do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("garbage: got %d", resp.StatusCode)
	}
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	resp.Body.Close()
	if !strings.Contains(string(buf[:n]), "-32700") {
		t.Fatalf("parse error code missing: %s", string(buf[:n]))
	}

	// JSON-RPC batch array (removed in MCP 2025-06-18) → parse error.
	req2, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := ts.Client().Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("batch: got %d, want 400", resp2.StatusCode)
	}
}

func TestMCP_ServerErrorsAreToolResults(t *testing.T) {
	ts, _, token := newStack(t, []string{model.CapAccountsManage})

	// Validation failure on the server (missing required fields) surfaces as
	// an isError tool result, not a protocol error.
	_, body := rpc(t, ts, token, "tools/call",
		map[string]any{"name": "createAccount", "arguments": map[string]any{
			"body": map[string]any{"code": "9-0001"},
		}}, 1)
	if !strings.Contains(body, "isError") || !strings.Contains(body, "422") {
		t.Fatalf("server 422 should surface as isError result: %s", body)
	}
}

var _ = fmt.Sprintf
