package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/naufal/latasya-erp/internal/account"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	v1accounts "github.com/naufal/latasya-erp/internal/api/v1/accounts"
	v1auth "github.com/naufal/latasya-erp/internal/api/v1/auth"
	v1income "github.com/naufal/latasya-erp/internal/api/v1/income"
	v1mcpauth "github.com/naufal/latasya-erp/internal/api/v1/mcpauth"
	"github.com/naufal/latasya-erp/internal/idempotency"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/mcpauth"
	"github.com/naufal/latasya-erp/internal/mcptools"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// TestGeneratedToolsCoverTheSpec is the coverage contract: every spec
// operation is either a generated tool or explicitly excluded, and nothing
// exists on both sides. A new endpoint fails here until classified.
func TestGeneratedToolsCoverTheSpec(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}

	specOps := map[string]bool{}
	for _, item := range doc.Paths.Map() {
		for _, op := range []*openapi3.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op != nil {
				specOps[op.OperationID] = true
			}
		}
	}

	generated := map[string]bool{}
	for _, tool := range mcptools.GeneratedTools {
		if !specOps[tool.Name] {
			t.Errorf("generated tool %q does not exist in the spec", tool.Name)
		}
		generated[tool.Name] = true
	}
	for name := range mcptools.ExcludedOperations {
		if !specOps[name] {
			t.Errorf("excluded operation %q does not exist in the spec", name)
		}
		if generated[name] {
			t.Errorf("operation %q is both generated and excluded", name)
		}
	}
	for name := range specOps {
		if _, excluded := mcptools.ExcludedOperations[name]; !generated[name] && !excluded {
			t.Errorf("operation %q is neither generated nor excluded — classify it", name)
		}
	}
	if len(generated) < 70 {
		t.Errorf("suspiciously few generated tools: %d", len(generated))
	}
}

// e2e drives the client against the real ERP API in-process, wired like
// cmd/server/main.go.
type e2e struct {
	t       *testing.T
	db      *sql.DB
	erp     *httptest.Server
	disp    *dispatcher
	token   string
	scopes  []string
	tokenID int
}

func newE2E(t *testing.T, scopes []string) *e2e {
	t.Helper()
	db := testutil.SetupTestDB(t)

	apiMux := http.NewServeMux()
	v1auth.New(db, true).RegisterRoutes(apiMux)
	(&v1accounts.Handler{Accounts: account.New(db)}).RegisterRoutes(apiMux)

	mcpMod := mcpauth.New(db)
	deviceAPI := &v1mcpauth.Handler{MCP: mcpMod}

	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/mcp/device", v1.MCPDeviceLimiter()(http.HandlerFunc(deviceAPI.DevicePoll)))
	mux.Handle("/api/v1/", v1.BearerOrCookie(db)(apiMux))
	erp := httptest.NewServer(mux)
	t.Cleanup(erp.Close)

	// The device login, end to end: register, approve directly through the
	// module (the consent page has its own tests), poll until minted.
	e := &e2e{t: t, db: db, erp: erp, scopes: scopes}
	e.loginViaDevice(mcpMod)
	return e
}

func (e *e2e) loginViaDevice(m *mcpauth.Module) {
	e.t.Helper()
	dev := "e2e-device-code-0000000000000000000000"
	code := "E2E12345"
	body, _ := json.Marshal(map[string]string{
		"device_code": dev, "user_code": code, "client": "latasya-mcp/1.0 (test)",
	})
	resp, err := e.erp.Client().Post(e.erp.URL+"/api/v1/mcp/device", "application/json", bytes.NewReader(body))
	if err != nil {
		e.t.Fatalf("register poll: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		e.t.Fatalf("register poll: got %d, want 202", resp.StatusCode)
	}

	if err := m.Approve(e.t.Context(), code, 1, "admin", e.scopes, nil); err != nil {
		e.t.Fatalf("approve: %v", err)
	}

	resp2, err := e.erp.Client().Post(e.erp.URL+"/api/v1/mcp/device", "application/json", bytes.NewReader(body))
	if err != nil {
		e.t.Fatalf("consume poll: %v", err)
	}
	defer resp2.Body.Close()
	var env struct {
		Data struct {
			Token   string   `json:"token"`
			TokenID int      `json:"token_id"`
			Scopes  []string `json:"scopes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&env); err != nil || env.Data.Token == "" {
		e.t.Fatalf("consume poll: status=%d err=%v", resp2.StatusCode, err)
	}
	e.token = env.Data.Token
	e.tokenID = env.Data.TokenID
	e.disp = &dispatcher{client: newAPIClient(e.erp.URL, e.token), tokenID: e.tokenID, scopes: env.Data.Scopes}
}

// call runs one tools/call through the dispatcher.
func (e *e2e) call(name string, args map[string]any) map[string]any {
	e.t.Helper()
	return e.disp.callTool(name, args)
}

func (e *e2e) callIsError(name string, args map[string]any) (map[string]any, bool) {
	e.t.Helper()
	res := e.call(name, args)
	isErr, _ := res["isError"].(bool)
	return res, isErr
}

func resultText(res map[string]any) string {
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func TestE2E_LoginToolsListScopedByCapability(t *testing.T) {
	e := newE2E(t, []string{model.CapAccountsManage, model.CapReportsView})

	s := &mcpServer{disp: e.disp}
	list := s.toolsList()
	tools := list["tools"].([]mcptools.ListEntry)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	if !names["createAccount"] {
		t.Error("accounts.manage token must see createAccount")
	}
	if names["createInvoice"] {
		t.Error("token without invoices.manage must not see createInvoice")
	}
	if names["getInvoicePDF"] {
		t.Error("token without invoices.manage must not see getInvoicePDF")
	}
	if len(tools) == 0 || len(tools) >= len(mcptools.GeneratedTools) {
		t.Errorf("tools/list not filtered: %d of %d", len(tools), len(mcptools.GeneratedTools))
	}
}

func TestE2E_CreateIncomeAndIdempotentRetry(t *testing.T) {
	// Income routes are idempotency-wrapped server-side, so the client's
	// deterministic key must make an identical retry replay instead of
	// duplicating the financial entry.
	db := testutil.SetupTestDB(t)
	idem := v1.Idempotency(idempotency.New(db))
	apiMux := http.NewServeMux()
	v1auth.New(db, true).RegisterRoutes(apiMux)
	(&v1income.Handler{Journals: journal.New(db)}).RegisterRoutes(apiMux, idem)
	mcpMod := mcpauth.New(db)
	deviceAPI := &v1mcpauth.Handler{MCP: mcpMod}
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/mcp/device", v1.MCPDeviceLimiter()(http.HandlerFunc(deviceAPI.DevicePoll)))
	mux.Handle("/api/v1/", v1.BearerOrCookie(db)(apiMux))
	erp := httptest.NewServer(mux)
	t.Cleanup(erp.Close)

	e := &e2e{t: t, db: db, erp: erp, scopes: []string{model.CapIncomeManage}}
	e.loginViaDevice(mcpMod)

	var cashID, revenueID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE account_type='asset' LIMIT 1").Scan(&cashID); err != nil {
		t.Fatalf("seed cash account: %v", err)
	}
	if err := db.QueryRow("SELECT id FROM accounts WHERE account_type='revenue' LIMIT 1").Scan(&revenueID); err != nil {
		t.Fatalf("seed revenue account: %v", err)
	}

	args := func() map[string]any {
		return map[string]any{
			"body": map[string]any{
				"entry_date":      "2026-11-09",
				"description":     "MCP e2e income",
				"amount":          "50000",
				"revenue_account": revenueID,
				"deposit_account": cashID,
			},
		}
	}
	res, isErr := e.callIsError("createIncome", args())
	if isErr {
		t.Fatalf("createIncome failed: %s", resultText(res))
	}

	retry, isErr := e.callIsError("createIncome", args())
	if isErr {
		t.Fatalf("idempotent retry failed: %s", resultText(retry))
	}
	// A true replay returns the stored 2xx response verbatim: the retry's
	// body must equal the original's (same id, no second entry created).
	if first, again := resultText(res), resultText(retry); first != again {
		t.Fatalf("identical retry did not replay:\nfirst: %s\nretry: %s", first, again)
	}
}

func TestE2E_CapabilityEnforcedByServer(t *testing.T) {
	// The client-side tools/list filter is UX; the server still rejects a
	// direct call without the capability.
	e := newE2E(t, []string{model.CapReportsView})
	res, isErr := e.callIsError("createAccount", map[string]any{
		"body": map[string]any{"code": "1-8888", "account_type": "asset"},
	})
	if !isErr {
		t.Fatalf("createAccount without accounts.manage must fail, got: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "403") {
		t.Errorf("expected 403 from the server in the error, got: %s", resultText(res))
	}
}

func TestE2E_UnknownToolIsErrorResult(t *testing.T) {
	e := newE2E(t, []string{model.CapReportsView})
	res, isErr := e.callIsError("doesNotExist", nil)
	if !isErr || !strings.Contains(resultText(res), "unknown tool") {
		t.Fatalf("unknown tool must be an isError result, got: %v", res)
	}
}

func TestE2E_MissingPathParamIsErrorResult(t *testing.T) {
	e := newE2E(t, []string{model.CapAccountsManage})
	res, isErr := e.callIsError("getAccount", map[string]any{})
	if !isErr || !strings.Contains(resultText(res), "id") {
		t.Fatalf("missing required param must be an isError result, got: %s", resultText(res))
	}
}

// TestStdioProtocol drives the JSON-RPC loop end to end over pipes.
func TestStdioProtocol(t *testing.T) {
	e := newE2E(t, []string{model.CapAccountsManage, model.CapReportsView})

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"listAccounts","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"bogus/method"}`,
		`not json at all`,
		"\n",
	}, "\n")

	var out bytes.Buffer
	s := &mcpServer{disp: e.disp}
	s.serve(strings.NewReader(input), &out)

	lines := nonEmptyLines(out.String())
	// 5 request responses + 1 parse-error response (id null).
	if len(lines) != 6 {
		t.Fatalf("expected 6 responses, got %d: %s", len(lines), out.String())
	}

	var init jsonrpcResponse
	json.Unmarshal([]byte(lines[0]), &init)
	m, _ := init.Result.(map[string]any)
	if m["protocolVersion"] != protocolVersion {
		t.Errorf("initialize protocolVersion: %v", m["protocolVersion"])
	}

	var list jsonrpcResponse
	json.Unmarshal([]byte(lines[2]), &list)
	if list.Result == nil {
		t.Errorf("tools/list response missing result: %s", lines[2])
	}

	var call jsonrpcResponse
	json.Unmarshal([]byte(lines[3]), &call)
	if call.Error != nil {
		t.Errorf("listAccounts call failed: %v", call.Error.Message)
	}
}

func TestRedactHidesToken(t *testing.T) {
	in := "bearer lat_SECRETVALUE failed"
	got := redact(in, "lat_SECRETVALUE")
	if strings.Contains(got, "SECRETVALUE") {
		t.Fatalf("token leaked: %s", got)
	}
}

func TestTextResultTruncates(t *testing.T) {
	res := textResult(bytes.Repeat([]byte("x"), 100_000))
	text := resultText(res)
	if len(text) > (64<<10)+64 {
		t.Fatalf("truncation missing: %d bytes", len(text))
	}
}

func TestPDFResultBlobAndLimit(t *testing.T) {
	small := pdfResult("/api/v1/invoices/1/pdf", []byte("%PDF-1.4 fake"))
	content := small["content"].([]any)
	res, _ := content[1].(map[string]any)
	resource, _ := res["resource"].(map[string]any)
	if resource["mimeType"] != "application/pdf" {
		t.Fatalf("resource shape wrong: %v", resource)
	}

	big := pdfResult("/api/v1/invoices/1/pdf", bytes.Repeat([]byte("x"), (2<<20)+1))
	if _, isErr := big["isError"]; !isErr {
		t.Fatal("oversize PDF must be an error result")
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	return out
}

var _ = fmt.Sprintf // keep fmt when subtests are trimmed
