package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// testServerWithAPITokens sets up a test HTTP server with only the API tokens
// routes wired up (plus login and password-change for the auth middleware chain).
func testServerWithAPITokens(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)

	mux := http.NewServeMux()
	h.RegisterAuthRoutes(mux, func(next http.Handler) http.Handler { return next })

	protected := http.NewServeMux()
	h.RegisterAccessRoutes(protected)
	h.RegisterSettingsRoutes(protected)

	mux.Handle("/", auth.RequireAuth(db, access.New(db, nil), auth.CSRFProtect(h.EnforcePasswordChange(protected))))

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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect (token not found for this owner), got %d", resp.StatusCode)
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
	t.Parallel()
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
	t.Parallel()
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

func TestAPITokens_NonAdminSelfService(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	cookies := loginAsBookkeeper(t, ts, db)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	for _, route := range []string{"/settings/api-tokens", "/settings/api-tokens/new"} {
		req, _ := requestWithCookies(db, "GET", ts.URL+route, cookies, "")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", route, resp.StatusCode)
		}
		if route == "/settings/api-tokens/new" {
			if !strings.Contains(string(body), `value="income.manage"`) {
				t.Error("new token form should offer the bookkeeper's own income.manage scope")
			}
			if strings.Contains(string(body), `value="users.manage"`) {
				t.Error("new token form must not offer users.manage to a bookkeeper")
			}
		}
	}

	// A scope outside the bookkeeper's role is rejected and no token is created.
	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, "name=hack&scopes=users.manage")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST forbidden scope: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST with users.manage: expected 200 form re-render, got %d", resp.StatusCode)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM api_tokens WHERE name='hack'").Scan(&count)
	if count != 0 {
		t.Errorf("expected no token for unauthorized scope, got %d", count)
	}

	// An own scope succeeds and the token belongs to the bookkeeper.
	req, _ = requestWithCookies(db, "POST", ts.URL+"/settings/api-tokens", cookies, "name=Telegram&scopes=income.manage")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST own scope: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("POST with income.manage: expected 303, got %d", resp.StatusCode)
	}
	var owner string
	db.QueryRow("SELECT u.username FROM api_tokens t JOIN users u ON u.id=t.user_id WHERE t.name='Telegram'").Scan(&owner)
	if owner != "bookkeeper" {
		t.Errorf("token owner: got %q, want bookkeeper", owner)
	}
}

// seedBookkeeperToken gives the bookkeeper (see loginAsBookkeeper) a token.
func seedBookkeeperToken(t *testing.T, db *sql.DB) int {
	t.Helper()
	db.Exec(`INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, scopes)
		SELECT id, 'telegram', 'lat_BKBK', 'hash-bk', '["income.manage"]' FROM users WHERE username='bookkeeper'`)
	var id int
	if err := db.QueryRow(`SELECT id FROM api_tokens WHERE token_hash='hash-bk'`).Scan(&id); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return id
}

func seedAdminToken(t *testing.T, db *sql.DB) int {
	t.Helper()
	db.Exec(`INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, scopes)
		VALUES (1, 'admin-token', 'lat_ADMN', 'hash-admin', '["reports.view"]')`)
	var id int
	if err := db.QueryRow(`SELECT id FROM api_tokens WHERE token_hash='hash-admin'`).Scan(&id); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return id
}

func tokenRevoked(db *sql.DB, id int) bool {
	var at sql.NullString
	db.QueryRow(`SELECT revoked_at FROM api_tokens WHERE id=?`, id).Scan(&at)
	return at.Valid
}

// sendRequest makes a CSRF-carrying request without following redirects.
func sendRequest(t *testing.T, db *sql.DB, cookies []*http.Cookie, method, url, form string) (*http.Response, string) {
	t.Helper()
	req, err := requestWithCookies(db, method, url, cookies, form)
	if err != nil {
		t.Fatalf("requestWithCookies: %v", err)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func flashOf(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == "flash" {
			return c.Value
		}
	}
	return ""
}

func TestListAPITokens_AllTokensShowEveryOwner(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	loginAsBookkeeper(t, ts, db)
	tokenID := seedBookkeeperToken(t, db)

	resp, body := sendRequest(t, db, loginAsAdmin(t, ts), "GET", ts.URL+"/settings/api-tokens", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	for _, want := range []string{"bookkeeper", "Bookkeeper User", "lat_BKBK", fmt.Sprintf(`action="/settings/api-tokens/all/%d/revoke"`, tokenID)} {
		if !strings.Contains(body, want) {
			t.Errorf("all users' tokens section missing %q", want)
		}
	}
}

func TestListAPITokens_AllTokensOnlyForUsersManage(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	bookkeeper := loginAsBookkeeper(t, ts, db)
	seedAdminToken(t, db)
	resp, body := sendRequest(t, db, bookkeeper, "GET", ts.URL+"/settings/api-tokens", "")
	if resp.StatusCode != http.StatusOK || strings.Contains(body, "All Users") || strings.Contains(body, "lat_ADMN") {
		t.Errorf("bookkeeper sees other users' tokens (status %d)", resp.StatusCode)
	}
	if _, body := sendRequest(t, db, loginAsAdmin(t, ts), "GET", ts.URL+"/users", ""); !strings.Contains(body, `href="/settings/api-tokens#all-tokens"`) {
		t.Error("users page has no link to all tokens")
	}
}

func TestListAPITokens_LoadError(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	admin := loginAsAdmin(t, ts)
	if _, err := db.Exec(`ALTER TABLE api_tokens RENAME TO api_tokens_gone`); err != nil {
		t.Fatal(err)
	}
	resp, body := sendRequest(t, db, admin, "GET", ts.URL+"/settings/api-tokens", "")
	if resp.StatusCode != http.StatusOK || strings.Count(body, "Failed to load tokens") != 2 {
		t.Errorf("expected 200 with an error in both sections, got %d", resp.StatusCode)
	}
}

// Called directly: the auth middleware itself needs the roles table.
func TestListAPITokens_RolesLoadError(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)
	db.Exec(`INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, scopes) VALUES (1, 'ops', 'lat_OPSX', 'hash-ops', '[]')`)
	if _, err := db.Exec(`ALTER TABLE roles RENAME TO roles_gone`); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin}
	rec := httptest.NewRecorder()
	h.ListAPITokens(rec, httptest.NewRequest("GET", "/settings/api-tokens", nil).WithContext(auth.WithUser(context.Background(), admin)))
	body := rec.Body.String()
	if !strings.Contains(body, "Failed to load roles") || !strings.Contains(body, "lat_OPSX") || strings.Contains(body, "/all/") {
		t.Errorf("expected the tokens listed, an error, and no Revoke on other users' tokens (status %d)", rec.Code)
	}
}

func TestRevokeAnyAPIToken_HappyPath(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	loginAsBookkeeper(t, ts, db)
	tokenID := seedBookkeeperToken(t, db)
	admin := loginAsAdmin(t, ts)

	resp, _ := sendRequest(t, db, admin, "POST", fmt.Sprintf("%s/settings/api-tokens/all/%d/revoke", ts.URL, tokenID), "")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/settings/api-tokens#all-tokens" || flashOf(resp) != "Token revoked" {
		t.Errorf("expected 303 to /settings/api-tokens#all-tokens with \"Token revoked\", got %d → %q flash %q",
			resp.StatusCode, resp.Header.Get("Location"), flashOf(resp))
	}
	if !tokenRevoked(db, tokenID) {
		t.Fatal("token not revoked")
	}
	var actor, metadata string
	db.QueryRow(`SELECT actor_username, metadata FROM audit_log WHERE action='api_token.revoke' AND target_id=?`, tokenID).Scan(&actor, &metadata)
	if actor != "admin" || !strings.Contains(metadata, `"owner_username":"bookkeeper"`) {
		t.Errorf("audit: actor %q metadata %s, want admin revoking bookkeeper's token", actor, metadata)
	}
	if _, body := sendRequest(t, db, admin, "GET", ts.URL+"/settings/api-tokens", ""); !strings.Contains(body, `badge-error badge-sm">Revoked`) {
		t.Error("revoked token not marked on the list")
	}
}

func TestRevokeAnyAPIToken_ForbiddenWithoutUsersManage(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	bookkeeper := loginAsBookkeeper(t, ts, db)
	tokenID := seedAdminToken(t, db)

	resp, _ := sendRequest(t, db, bookkeeper, "POST", fmt.Sprintf("%s/settings/api-tokens/all/%d/revoke", ts.URL, tokenID), "")
	if resp.StatusCode != http.StatusForbidden || tokenRevoked(db, tokenID) {
		t.Errorf("expected 403 and the token kept, got %d revoked=%v", resp.StatusCode, tokenRevoked(db, tokenID))
	}
}

func TestRevokeAnyAPIToken_AdminTokenOutOfReach(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	manager, _ := loginAsUserManager(t, ts, db)
	tokenID := seedAdminToken(t, db)

	resp, _ := sendRequest(t, db, manager, "POST", fmt.Sprintf("%s/settings/api-tokens/all/%d/revoke", ts.URL, tokenID), "")
	if resp.StatusCode != http.StatusSeeOther || flashOf(resp) != "Cannot revoke a token of a user with more permissions than you" {
		t.Errorf("expected 303 with a refusal flash, got %d %q", resp.StatusCode, flashOf(resp))
	}
	if tokenRevoked(db, tokenID) {
		t.Error("admin's token revoked by a non-admin manager")
	}
}

func TestListAPITokens_HidesRevokeBeyondOwnPermissions(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	manager, _ := loginAsUserManager(t, ts, db)
	peerID := testutil.CreateTestUser(t, db, "peer", "pw", "manager-role")
	adminTokenID := seedAdminToken(t, db)
	db.Exec(`INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, scopes)
		VALUES (?, 'peer-token', 'lat_PEER', 'hash-peer', '["users.manage"]')`, peerID)
	var peerTokenID int
	db.QueryRow(`SELECT id FROM api_tokens WHERE token_hash='hash-peer'`).Scan(&peerTokenID)
	revokeAction := func(id int) string { return fmt.Sprintf(`action="/settings/api-tokens/all/%d/revoke"`, id) }

	_, body := sendRequest(t, db, manager, "GET", ts.URL+"/settings/api-tokens", "")
	if !strings.Contains(body, "lat_ADMN") || !strings.Contains(body, revokeAction(peerTokenID)) {
		t.Error("manager should see every token and be able to revoke the peer's")
	}
	if strings.Contains(body, revokeAction(adminTokenID)) {
		t.Error("manager is shown Revoke on the admin's token")
	}
	if _, body := sendRequest(t, db, loginAsAdmin(t, ts), "GET", ts.URL+"/settings/api-tokens", ""); !strings.Contains(body, revokeAction(adminTokenID)) || !strings.Contains(body, revokeAction(peerTokenID)) {
		t.Error("admin should see Revoke on every active token")
	}
}

func TestRevokeAnyAPIToken_NoCSRF(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	loginAsBookkeeper(t, ts, db)
	tokenID := seedBookkeeperToken(t, db)

	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/settings/api-tokens/all/%d/revoke", ts.URL, tokenID), nil)
	for _, c := range loginAsAdmin(t, ts) {
		req.AddCookie(c)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || tokenRevoked(db, tokenID) {
		t.Errorf("expected 403 (CSRF required) and the token kept, got %d", resp.StatusCode)
	}
}

func TestRevokeAnyAPIToken_NonexistentID(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	resp, _ := sendRequest(t, db, loginAsAdmin(t, ts), "POST", ts.URL+"/settings/api-tokens/all/99999/revoke", "")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/settings/api-tokens#all-tokens" || flashOf(resp) != "Token not found" {
		t.Errorf("expected 303 to /settings/api-tokens#all-tokens with \"Token not found\", got %d → %q flash %q",
			resp.StatusCode, resp.Header.Get("Location"), flashOf(resp))
	}
}

func TestRevokeAPIToken_InvalidID(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	admin := loginAsAdmin(t, ts)
	for _, path := range []string{"/settings/api-tokens/abc/revoke", "/settings/api-tokens/0/revoke",
		"/settings/api-tokens/all/abc/revoke", "/settings/api-tokens/all/-1/revoke"} {
		if resp, _ := sendRequest(t, db, admin, "POST", ts.URL+path, ""); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST %s: expected 400, got %d", path, resp.StatusCode)
		}
	}
}

// A database failure is reported as such, not as "not found", and each route
// returns to its own table.
func TestRevokeAPIToken_DatabaseError(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithAPITokens(t)
	admin := loginAsAdmin(t, ts)
	if _, err := db.Exec(`ALTER TABLE api_tokens RENAME TO api_tokens_gone`); err != nil {
		t.Fatal(err)
	}
	for path, back := range map[string]string{"/settings/api-tokens/1/revoke": "/settings/api-tokens", "/settings/api-tokens/all/1/revoke": "/settings/api-tokens#all-tokens"} {
		resp, _ := sendRequest(t, db, admin, "POST", ts.URL+path, "")
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != back || flashOf(resp) != "Failed to revoke token" {
			t.Errorf("POST %s: expected 303 to %s with \"Failed to revoke token\", got %d → %q flash %q",
				path, back, resp.StatusCode, resp.Header.Get("Location"), flashOf(resp))
		}
	}
}

// The handlers send a request without a user to the login page. The auth
// middleware normally catches this first; the handlers must not panic.
func TestAPITokenHandlers_NoUser(t *testing.T) {
	t.Parallel()
	h := testutil.SetupTestHandler(t, testutil.SetupTestDB(t))
	for name, handle := range map[string]http.HandlerFunc{"own revoke": h.RevokeAPIToken, "any revoke": h.RevokeAnyAPIToken, "list": h.ListAPITokens} {
		rec := httptest.NewRecorder()
		handle(rec, httptest.NewRequest("POST", "/revoke", nil))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
			t.Errorf("%s: %d → %q, want 303 to /login", name, rec.Code, rec.Header().Get("Location"))
		}
	}
}
