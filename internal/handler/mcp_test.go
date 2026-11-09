package handler_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/mcpauth"
	"github.com/naufal/latasya-erp/internal/model"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func bodyText(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestMCPAuthorizePage_ShowsClientAndScopes(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)

	m := mcpauth.New(db)
	if _, err := m.Touch(t.Context(), sha256Hex("device-1"), "AB12CD34", "latasya-mcp/1.0 (darwin/arm64)"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/mcp/authorize?code=AB12CD34", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	body := bodyText(t, resp)
	for _, want := range []string{"latasya-mcp/1.0 (darwin/arm64)", "AB12CD34", model.CapInvoicesManage, "Approve"} {
		if !strings.Contains(body, want) {
			t.Errorf("consent page missing %q", want)
		}
	}
}

func TestMCPAuthorize_ApproveStoresDecisionAndAudits(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)

	m := mcpauth.New(db)
	if _, err := m.Touch(t.Context(), sha256Hex("device-2"), "APPROV01", "latasya-mcp/1.0 (test)"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"code":        {"APPROV01"},
		"client":      {"latasya-mcp/1.0 (test)"},
		"scopes":      {model.CapReportsView},
		"expiry_days": {"90"},
		"action":      {"approve"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/mcp/authorize", cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status: got %d, want 303", resp.StatusCode)
	}

	var status, scopes string
	if err := db.QueryRow("SELECT status, scopes FROM mcp_handoffs WHERE user_code='APPROV01'").Scan(&status, &scopes); err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	if status != "approved" {
		t.Errorf("handoff status: got %q, want approved", status)
	}
	if !strings.Contains(scopes, model.CapReportsView) {
		t.Errorf("handoff scopes: got %q", scopes)
	}

	var action string
	if err := db.QueryRow("SELECT action FROM audit_log WHERE action='mcp.handoff.approve'").Scan(&action); err != nil {
		t.Errorf("audit row missing: %v", err)
	}
}

func TestMCPAuthorize_DenyBlocksHandoff(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)

	m := mcpauth.New(db)
	if _, err := m.Touch(t.Context(), sha256Hex("device-3"), "DENYME01", "latasya-mcp/1.0 (test)"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	cookies := loginAsAdmin(t, ts)

	form := url.Values{"code": {"DENYME01"}, "action": {"deny"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/mcp/authorize", cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status: got %d, want 303", resp.StatusCode)
	}

	var status string
	if err := db.QueryRow("SELECT status FROM mcp_handoffs WHERE user_code='DENYME01'").Scan(&status); err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	if status != "denied" {
		t.Errorf("handoff status: got %q, want denied", status)
	}
}

func TestMCPAuthorize_ScopeBeyondOwnCapabilitiesDropped(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)

	m := mcpauth.New(db)
	if _, err := m.Touch(t.Context(), sha256Hex("device-4"), "SMUGGL01", "latasya-mcp/1.0 (test)"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	// viewer holds only reports.view; the form tries to smuggle users.manage.
	cookies := loginAsViewer(t, ts, db)

	form := url.Values{
		"code":   {"SMUGGL01"},
		"scopes": {model.CapUsersManage, model.CapReportsView},
		"action": {"approve"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/mcp/authorize", cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var scopes string
	if err := db.QueryRow("SELECT scopes FROM mcp_handoffs WHERE user_code='SMUGGL01'").Scan(&scopes); err != nil {
		t.Fatalf("load handoff: %v", err)
	}
	if strings.Contains(scopes, model.CapUsersManage) {
		t.Errorf("smuggled scope stored: %q", scopes)
	}
	if !strings.Contains(scopes, model.CapReportsView) {
		t.Errorf("legitimate scope dropped: %q", scopes)
	}
}

func TestMCPAuthorize_UnknownCodeRedirectsHome(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/mcp/authorize?code=NOPE0000", cookies, "")
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status: got %d, want 303", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("Location: got %q, want \"/\"", loc)
	}
}

func TestMCPAuthorize_RequiresCSRF(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)

	m := mcpauth.New(db)
	if _, err := m.Touch(t.Context(), sha256Hex("device-5"), "CSRFTST1", "latasya-mcp/1.0 (test)"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	cookies := loginAsAdmin(t, ts)

	form := url.Values{"code": {"CSRFTST1"}, "action": {"approve"}}
	req, _ := http.NewRequest("POST", ts.URL+"/mcp/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("post without csrf: got %d, want 403", resp.StatusCode)
	}
}

var _ *sql.DB // silence unused-import churn during refactors
