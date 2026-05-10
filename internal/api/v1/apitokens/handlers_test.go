package apitokens_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/api/v1/apitokens"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &apitokens.Handler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/api-tokens", h.Create)
	mux.HandleFunc("GET /api/v1/api-tokens", h.List)
	mux.HandleFunc("DELETE /api/v1/api-tokens/{id}", h.Revoke)
	ts := httptest.NewServer(v1.BearerOrCookie(db)(mux))
	t.Cleanup(ts.Close)
	return ts
}

func adminID(t *testing.T, db *sql.DB) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&id); err != nil {
		t.Fatalf("get admin id: %v", err)
	}
	return id
}

func cookieFor(t *testing.T, db *sql.DB, userID int) *http.Cookie {
	t.Helper()
	sid := testutil.CreateTestSession(t, db, userID)
	return &http.Cookie{Name: "session_id", Value: sid}
}

func bearerFor(t *testing.T, db *sql.DB, userID int, scopes []string) string {
	t.Helper()
	_, plaintext, err := model.CreateAPIToken(db, userID,
		fmt.Sprintf("test-%d", time.Now().UnixNano()), scopes, nil)
	if err != nil {
		t.Fatalf("create bearer: %v", err)
	}
	return plaintext
}

type reqOpts struct {
	cookie *http.Cookie
	bearer string
	body   any
}

func do(t *testing.T, ts *httptest.Server, method, path string, opts reqOpts) *http.Response {
	t.Helper()
	var bodyBytes []byte
	if opts.body != nil {
		var err error
		bodyBytes, err = json.Marshal(opts.body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if opts.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if opts.cookie != nil {
		req.AddCookie(opts.cookie)
	}
	if opts.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+opts.bearer)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

type tokenView struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
	Plaintext  string     `json:"plaintext,omitempty"`
}

type singleEnv struct {
	Data tokenView `json:"data"`
}

type listEnv struct {
	Data []tokenView `json:"data"`
}

type errEnv struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func TestCreateToken_Success(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	resp := do(t, ts, http.MethodPost, "/api/v1/api-tokens", reqOpts{
		cookie: cookieFor(t, db, adminID(t, db)),
		body: map[string]any{
			"name":   "Telegram Bot",
			"scopes": []string{model.CapReportsView},
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		var e errEnv
		json.NewDecoder(resp.Body).Decode(&e) //nolint:errcheck
		t.Fatalf("expected 201, got %d: %+v", resp.StatusCode, e)
	}
	var env singleEnv
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(env.Data.Plaintext, "lat_") {
		t.Errorf("plaintext should start with lat_, got %q", env.Data.Plaintext)
	}
	if env.Data.Prefix == "" || len(env.Data.Prefix) != 8 {
		t.Errorf("prefix should be 8 chars, got %q", env.Data.Prefix)
	}
	if env.Data.Name != "Telegram Bot" {
		t.Errorf("name mismatch: %q", env.Data.Name)
	}
	if len(env.Data.Scopes) != 1 || env.Data.Scopes[0] != model.CapReportsView {
		t.Errorf("scopes mismatch: %v", env.Data.Scopes)
	}
}

func TestCreateToken_BearerRejected(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	bearer := bearerFor(t, db, adminID(t, db), []string{model.CapReportsView})

	resp := do(t, ts, http.MethodPost, "/api/v1/api-tokens", reqOpts{
		bearer: bearer,
		body: map[string]any{
			"name":   "Spawned Token",
			"scopes": []string{model.CapReportsView},
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	var e errEnv
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != v1.CodeForbidden {
		t.Errorf("expected code=forbidden, got %q", e.Code)
	}
}

func TestCreateToken_ScopeOverreach(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	viewerID := testutil.CreateTestUser(t, db, "viewer-tok", "pw", "viewer")

	resp := do(t, ts, http.MethodPost, "/api/v1/api-tokens", reqOpts{
		cookie: cookieFor(t, db, viewerID),
		body: map[string]any{
			"name":   "Overreach",
			"scopes": []string{model.CapUsersManage},
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}

func TestCreateToken_AdminGrantsAny(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	resp := do(t, ts, http.MethodPost, "/api/v1/api-tokens", reqOpts{
		cookie: cookieFor(t, db, adminID(t, db)),
		body: map[string]any{
			"name":   "All Caps",
			"scopes": []string{model.CapUsersManage, model.CapRolesManage, model.CapAuditView},
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for admin, got %d", resp.StatusCode)
	}
}

func TestCreateToken_MissingName(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	resp := do(t, ts, http.MethodPost, "/api/v1/api-tokens", reqOpts{
		cookie: cookieFor(t, db, adminID(t, db)),
		body: map[string]any{
			"scopes": []string{model.CapReportsView},
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}

func TestCreateToken_PastExpiryRejected(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	past := time.Now().UTC().Add(-time.Hour)
	resp := do(t, ts, http.MethodPost, "/api/v1/api-tokens", reqOpts{
		cookie: cookieFor(t, db, adminID(t, db)),
		body: map[string]any{
			"name":       "Expired",
			"scopes":     []string{model.CapReportsView},
			"expires_at": past.Format(time.RFC3339),
		},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
}

func TestListTokens_NeverShowsPlaintext(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	uid := adminID(t, db)

	if _, _, err := model.CreateAPIToken(db, uid, "alpha", []string{model.CapReportsView}, nil); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	resp := do(t, ts, http.MethodGet, "/api/v1/api-tokens", reqOpts{
		cookie: cookieFor(t, db, uid),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := json.Marshal(json.RawMessage{})
	dec := json.NewDecoder(resp.Body)
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	bs, _ := json.Marshal(raw)
	if strings.Contains(string(bs), "plaintext") {
		t.Errorf("list response must not contain 'plaintext': %s", string(bs))
	}
	if strings.Contains(string(bs), "token_hash") || strings.Contains(string(bs), "hash") {
		t.Errorf("list response must not contain hash field: %s", string(bs))
	}
	_ = body
}

func TestListTokens_BearerAllowed(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	uid := adminID(t, db)

	bearer := bearerFor(t, db, uid, []string{model.CapReportsView})

	resp := do(t, ts, http.MethodGet, "/api/v1/api-tokens", reqOpts{bearer: bearer})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var env listEnv
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data) == 0 {
		t.Fatal("expected at least the bearer token itself in list")
	}
	for _, tk := range env.Data {
		if tk.Plaintext != "" {
			t.Errorf("plaintext leaked in list response: %q", tk.Plaintext)
		}
	}
}

func TestRevokeToken_Success(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	uid := adminID(t, db)

	tok, _, err := model.CreateAPIToken(db, uid, "to-revoke", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	resp := do(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/api-tokens/%d", tok.ID), reqOpts{
		cookie: cookieFor(t, db, uid),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	var revokedAt sql.NullString
	if err := db.QueryRow(`SELECT revoked_at FROM api_tokens WHERE id = ?`, tok.ID).Scan(&revokedAt); err != nil {
		t.Fatalf("query revoked_at: %v", err)
	}
	if !revokedAt.Valid {
		t.Error("revoked_at should be set after revocation")
	}
}

func TestRevokeToken_BearerRejected(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	uid := adminID(t, db)

	bearer := bearerFor(t, db, uid, []string{model.CapReportsView})
	tok, _, err := model.CreateAPIToken(db, uid, "victim", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	resp := do(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/api-tokens/%d", tok.ID), reqOpts{
		bearer: bearer,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestRevokeToken_CrossUser(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	otherID := testutil.CreateTestUser(t, db, "other-user", "pw", "viewer")
	tok, _, err := model.CreateAPIToken(db, otherID, "other-tok", []string{}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	resp := do(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/api-tokens/%d", tok.ID), reqOpts{
		cookie: cookieFor(t, db, adminID(t, db)),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestRevokeToken_NonexistentReturns404(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	resp := do(t, ts, http.MethodDelete, "/api/v1/api-tokens/999999", reqOpts{
		cookie: cookieFor(t, db, adminID(t, db)),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
