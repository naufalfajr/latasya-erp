package handler_test

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// testServerWithAPITokens sets up a test HTTP server with only the API tokens
// routes wired up (plus login and password-change for the auth middleware chain).
func testServerWithAPITokens(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /settings/api-tokens", auth.AdminOnly(h.ListAPITokens))
	protected.HandleFunc("GET /settings/api-tokens/new", auth.AdminOnly(h.NewAPIToken))
	protected.HandleFunc("GET /settings/api-tokens/created", auth.AdminOnly(h.CreatedAPIToken))
	protected.HandleFunc("POST /settings/api-tokens", auth.AdminOnly(h.CreateAPIToken))
	protected.HandleFunc("POST /settings/api-tokens/{id}/revoke", auth.AdminOnly(h.RevokeAPIToken))
	protected.HandleFunc("GET /password/change", h.PasswordChangePage)
	protected.HandleFunc("POST /password/change", h.PasswordChange)

	mux.Handle("/", auth.RequireAuth(db, auth.CSRFProtect(h.EnforcePasswordChange(protected))))

	hash, err := auth.HashPassword(adminTestPassword)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	if _, err := db.Exec(
		"UPDATE users SET password=?, must_change_password=0 WHERE username='admin'",
		hash,
	); err != nil {
		t.Fatalf("update admin: %v", err)
	}

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, db
}

// --- API Tokens List Tests ---

func TestListAPITokens_RendersTable(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	// Seed one active token directly
	db.Exec(`INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, scopes)
		VALUES (1, 'test-token', 'lat_aBcD', 'fakehash123', '["reports.view"]')`)

	client := &http.Client{}
	req, err := requestWithCookies(db, "GET", ts.URL+"/settings/api-tokens", cookies, "")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "test-token") {
		t.Error("body missing token name 'test-token'")
	}
	if !strings.Contains(bodyStr, "lat_aBcD") {
		t.Error("body missing token prefix 'lat_aBcD'")
	}
}

func TestListAPITokens_EmptyState(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, err := requestWithCookies(db, "GET", ts.URL+"/settings/api-tokens", cookies, "")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "No API tokens yet.") {
		t.Error("expected empty-state message 'No API tokens yet.'")
	}
}

// --- New Token Form Tests ---

func TestNewAPIToken_RendersForm(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, err := requestWithCookies(db, "GET", ts.URL+"/settings/api-tokens/new", cookies, "")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	mustContain := []string{
		"Create API Token",
		`name="name"`,
		`name="scopes"`,
		`name="expires_at"`,
		`method="POST" action="/settings/api-tokens"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("body missing %q", want)
		}
	}

	mustNotContain := []string{"checked"}
	for _, bad := range mustNotContain {
		if strings.Contains(bodyStr, bad) {
			t.Errorf("body should not contain %q", bad)
		}
	}
}

// --- Create Token Tests ---

func TestCreateAPIToken_HappyPath(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, err := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, "name=test-token&scopes=reports.view")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "/settings/api-tokens/created" {
		t.Errorf("expected redirect to /settings/api-tokens/created, got %q", loc)
	}

	// Flash cookie should contain the plaintext token
	var flashCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "flash" {
			flashCookie = c
			break
		}
	}
	if flashCookie == nil {
		t.Fatal("expected flash cookie to be set")
	}
	tokenPattern := regexp.MustCompile(`^lat_[A-Za-z0-9]{32}$`)
	if !tokenPattern.MatchString(flashCookie.Value) {
		t.Errorf("flash value %q does not match expected token pattern lat_[A-Za-z0-9]{32}", flashCookie.Value)
	}

	// DB should have the token
	var count int
	db.QueryRow("SELECT COUNT(*) FROM api_tokens WHERE name='test-token'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 token named 'test-token' in DB, got %d", count)
	}
}

func TestCreateAPIToken_DuplicateName(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Create first token
	req1, _ := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, "name=dupe-token&scopes=reports.view")
	resp1, err := noRedirect.Do(req1)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	resp1.Body.Close()

	// Attempt to create with the same name
	req2, err := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, "name=dupe-token&scopes=reports.view")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp2, err := noRedirect.Do(req2)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render), got %d", resp2.StatusCode)
	}

	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), "already exists") {
		t.Error("expected 'already exists' error message in body")
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM api_tokens WHERE name='dupe-token'").Scan(&count)
	if count != 1 {
		t.Errorf("expected exactly 1 token named 'dupe-token', got %d", count)
	}
}

func TestCreateAPIToken_EmptyScopes(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// POST with name but no scopes field
	req, err := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, "name=no-scopes-token")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render), got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "at least one scope") {
		t.Error("expected 'at least one scope' error in body")
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM api_tokens").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 tokens in DB, got %d", count)
	}
}

func TestCreateAPIToken_EmptyName(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, err := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, "name=&scopes=accounts.view")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render), got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Name is required") {
		t.Error("expected 'Name is required' error in body")
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM api_tokens").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 tokens in DB, got %d", count)
	}
}

func TestCreateAPIToken_UnauthorizedScope(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	// Admin attempts to use an unrecognized scope
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, err := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, "name=bad-token&scopes=bogus.scope")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render), got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Unknown or unauthorized scope") {
		t.Error("expected 'Unknown or unauthorized scope' error in body")
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM api_tokens").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 tokens in DB, got %d", count)
	}
}

func TestCreateAPIToken_NoCSRF(t *testing.T) {
	ts, _ := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Manually create request — no CSRF token attached
	req, err := http.NewRequest("POST", ts.URL+"/settings/api-tokens",
		strings.NewReader("name=test&scopes=accounts.view"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 (CSRF required), got %d", resp.StatusCode)
	}
}

// --- Created Page Tests ---

func TestCreatedPage_WithFlash(t *testing.T) {
	ts, _ := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Manually set flash cookie with a fake plaintext token value
	const fakeToken = "lat_testtoken12345678901234567890"
	req, err := http.NewRequest("GET", ts.URL+"/settings/api-tokens/created", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.AddCookie(&http.Cookie{Name: "flash", Value: fakeToken})

	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), fakeToken) {
		t.Error("expected body to contain the plaintext token value")
	}

	cacheControl := resp.Header.Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-store") {
		t.Errorf("expected Cache-Control to contain 'no-store', got %q", cacheControl)
	}

	// Flash cookie should be cleared in the response
	var flashCleared bool
	for _, c := range resp.Cookies() {
		if c.Name == "flash" && (c.MaxAge < 0 || c.Value == "") {
			flashCleared = true
		}
	}
	if !flashCleared {
		t.Error("expected flash cookie to be cleared (MaxAge < 0 or empty value)")
	}
}

func TestCreatedPage_NoFlash(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// No flash cookie — handler should redirect back to list
	req, err := requestWithCookies(db, "GET", ts.URL+"/settings/api-tokens/created", cookies, "")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "/settings/api-tokens" {
		t.Errorf("expected redirect to /settings/api-tokens, got %q", loc)
	}
}

// --- Revoke Token Tests ---

func TestRevokeAPIToken_HappyPath(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	// Seed a token owned by admin
	db.Exec(`INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, scopes)
		VALUES (1, 'revoke-me', 'lat_XXXX', 'fakehash456', '["reports.view"]')`)
	var tokenID int
	db.QueryRow("SELECT id FROM api_tokens WHERE name='revoke-me'").Scan(&tokenID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	revokeURL := fmt.Sprintf("%s/settings/api-tokens/%d/revoke", ts.URL, tokenID)
	req, err := requestWithCookies(db, "POST", revokeURL, cookies, "")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc != "/settings/api-tokens" {
		t.Errorf("expected redirect to /settings/api-tokens, got %q", loc)
	}

	// Token should now be revoked
	var revokedAt sql.NullString
	db.QueryRow("SELECT revoked_at FROM api_tokens WHERE id=?", tokenID).Scan(&revokedAt)
	if !revokedAt.Valid {
		t.Error("expected revoked_at to be set after revoke")
	}

	// Audit log should record the revocation
	var auditCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='api_token.revoke'").Scan(&auditCount)
	if auditCount != 1 {
		t.Errorf("expected 1 audit_log entry for api_token.revoke, got %d", auditCount)
	}
}

func TestRevokeAPIToken_NotOwner(t *testing.T) {
	ts, db := testServerWithAPITokens(t)

	// Seed a token owned by admin (user_id=1)
	db.Exec(`INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, scopes)
		VALUES (1, 'admin-token', 'lat_YYYY', 'fakehash789', '["reports.view"]')`)
	var tokenID int
	db.QueryRow("SELECT id FROM api_tokens WHERE name='admin-token'").Scan(&tokenID)

	// Login as bookkeeper (different user — cannot revoke admin's token)
	cookies := loginAsBookkeeper(t, ts, db)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	revokeURL := fmt.Sprintf("%s/settings/api-tokens/%d/revoke", ts.URL, tokenID)
	req, err := requestWithCookies(db, "POST", revokeURL, cookies, "")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 (middleware blocks non-admin), got %d", resp.StatusCode)
	}

	// Token should NOT have been revoked (bug #9 regression guard)
	var revokedAt sql.NullString
	db.QueryRow("SELECT revoked_at FROM api_tokens WHERE id=?", tokenID).Scan(&revokedAt)
	if revokedAt.Valid {
		t.Error("token should NOT be revoked when a non-owner attempts revocation")
	}

	// Audit log must NOT record a revocation
	var auditCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='api_token.revoke'").Scan(&auditCount)
	if auditCount != 0 {
		t.Errorf("expected 0 audit_log entries for api_token.revoke, got %d", auditCount)
	}
}

func TestRevokeAPIToken_NonexistentID(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, err := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens/99999/revoke", cookies, "")
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}

	// No audit log entry should be created for a non-existent token
	var auditCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='api_token.revoke'").Scan(&auditCount)
	if auditCount != 0 {
		t.Errorf("expected 0 audit_log entries for api_token.revoke, got %d", auditCount)
	}
}

func TestRevokeAPIToken_NoCSRF(t *testing.T) {
	ts, _ := testServerWithAPITokens(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Manually create request — no CSRF token attached
	req, err := http.NewRequest("POST", ts.URL+"/settings/api-tokens/1/revoke", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 (CSRF required), got %d", resp.StatusCode)
	}
}

func TestAPITokens_NonAdminForbidden(t *testing.T) {
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsBookkeeper(t, ts, db)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	getRoutes := []string{
		"/settings/api-tokens",
		"/settings/api-tokens/new",
		"/settings/api-tokens/created",
	}
	for _, route := range getRoutes {
		req, _ := requestWithCookies(db, "GET", ts.URL+route, cookies, "")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("GET %s: expected 403, got %d", route, resp.StatusCode)
		}
	}

	form := "name=hack&scopes=reports.view"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, form)
	resp, _ := client.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST /settings/api-tokens: expected 403, got %d", resp.StatusCode)
	}

	req2, _ := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens/1/revoke", cookies, "")
	resp2, _ := client.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("POST /settings/api-tokens/1/revoke: expected 403, got %d", resp2.StatusCode)
	}
}
