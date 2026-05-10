package testutil

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// apiMatrixSeq provides unique suffixes for usernames created inside APIMatrix
// so that multiple calls within the same test (or parallel tests sharing a DB)
// never collide on the unique-username constraint.
var apiMatrixSeq struct {
	mu sync.Mutex
	n  int
}

func nextAPIMatrixSeq() int {
	apiMatrixSeq.mu.Lock()
	defer apiMatrixSeq.mu.Unlock()
	apiMatrixSeq.n++
	return apiMatrixSeq.n
}

// AuthMatrix defines expected HTTP status codes for each of the 8 auth scenarios.
// Use 0 to skip a scenario (not applicable for this endpoint).
type AuthMatrix struct {
	Anon                int // no auth at all
	ValidBearer         int // valid bearer token with correct scopes
	ExpiredBearer       int // expired bearer token
	RevokedBearer       int // revoked bearer token
	ScopeMissingBearer  int // valid bearer but missing required scope
	ValidCookieCSRF     int // valid session cookie + correct CSRF token
	ValidCookieNoCSRF   int // valid session cookie but missing/wrong CSRF token (for POST/DELETE)
	BearerMustChangePwd int // bearer token for user with must_change_password=true
}

// APIMatrix runs the same request up to 8 times with different auth conditions
// and asserts the expected HTTP status code for each non-zero scenario.
//
// db is used to create transient test tokens and sessions — it must be the
// same database backing ts. Bearer scenarios require the Bearer middleware
// (T6) wired into ts for non-trivial assertions; the helper creates auth
// artifacts regardless and the caller's AuthMatrix sets expected codes.
func APIMatrix(t *testing.T, ts *httptest.Server, db *sql.DB, method, path, body string, matrix AuthMatrix) {
	t.Helper()

	seq := nextAPIMatrixSeq()
	tokenSeq := 0

	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("APIMatrix: get admin user: %v", err)
	}

	createBearer := func(userID int, scopes []string, expiresAt *time.Time) string {
		t.Helper()
		tokenSeq++
		_, plaintext, err := model.CreateAPIToken(db, userID, fmt.Sprintf("apimatrix-%d-%d", seq, tokenSeq), scopes, expiresAt)
		if err != nil {
			t.Fatalf("APIMatrix: create api token: %v", err)
		}
		return plaintext
	}

	createSession := func(userID int) (string, string) {
		t.Helper()
		sessionID, err := auth.CreateSession(db, userID)
		if err != nil {
			t.Fatalf("APIMatrix: create session: %v", err)
		}
		csrf, err := auth.GetSessionCSRF(db, sessionID)
		if err != nil {
			t.Fatalf("APIMatrix: get session csrf: %v", err)
		}
		return sessionID, csrf
	}

	type scenario struct {
		name     string
		expected int
		setup    func(req *http.Request)
	}

	var scenarios []scenario

	if matrix.Anon != 0 {
		scenarios = append(scenarios, scenario{
			name:     "anon",
			expected: matrix.Anon,
			setup:    func(req *http.Request) {},
		})
	}

	if matrix.ValidBearer != 0 {
		token := createBearer(adminID, []string{
			"accounts.manage", "contacts.manage", "journals.manage",
			"income.manage", "expenses.manage", "invoices.manage", "bills.manage",
		}, nil)
		scenarios = append(scenarios, scenario{
			name:     "valid_bearer",
			expected: matrix.ValidBearer,
			setup: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+token)
			},
		})
	}

	if matrix.ExpiredBearer != 0 {
		past := time.Now().Add(-1 * time.Hour)
		token := createBearer(adminID, []string{"accounts.manage"}, &past)
		scenarios = append(scenarios, scenario{
			name:     "expired_bearer",
			expected: matrix.ExpiredBearer,
			setup: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+token)
			},
		})
	}

	if matrix.RevokedBearer != 0 {
		tokenSeq++
		tokenObj, plaintext, err := model.CreateAPIToken(db, adminID, fmt.Sprintf("apimatrix-%d-%d", seq, tokenSeq), []string{"accounts.manage"}, nil)
		if err != nil {
			t.Fatalf("APIMatrix: create revoked token: %v", err)
		}
		if err := model.RevokeAPIToken(db, adminID, tokenObj.ID); err != nil {
			t.Fatalf("APIMatrix: revoke token: %v", err)
		}
		revokedPlaintext := plaintext
		scenarios = append(scenarios, scenario{
			name:     "revoked_bearer",
			expected: matrix.RevokedBearer,
			setup: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+revokedPlaintext)
			},
		})
	}

	if matrix.ScopeMissingBearer != 0 {
		noScopeUserID := CreateTestUser(t, db, fmt.Sprintf("apim-noscope-%d", seq), "password123", "viewer")
		token := createBearer(noScopeUserID, []string{}, nil)
		scenarios = append(scenarios, scenario{
			name:     "scope_missing_bearer",
			expected: matrix.ScopeMissingBearer,
			setup: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+token)
			},
		})
	}

	if matrix.ValidCookieCSRF != 0 {
		sessionID, csrfToken := createSession(adminID)
		sid, csrf := sessionID, csrfToken
		scenarios = append(scenarios, scenario{
			name:     "valid_cookie_csrf",
			expected: matrix.ValidCookieCSRF,
			setup: func(req *http.Request) {
				req.AddCookie(&http.Cookie{Name: "session_id", Value: sid})
				req.Header.Set("X-CSRF-Token", csrf)
			},
		})
	}

	if matrix.ValidCookieNoCSRF != 0 {
		sessionID, _ := createSession(adminID)
		sid := sessionID
		scenarios = append(scenarios, scenario{
			name:     "valid_cookie_no_csrf",
			expected: matrix.ValidCookieNoCSRF,
			setup: func(req *http.Request) {
				req.AddCookie(&http.Cookie{Name: "session_id", Value: sid})
				// deliberately omit X-CSRF-Token to trigger CSRF rejection on state-changing methods
			},
		})
	}

	if matrix.BearerMustChangePwd != 0 {
		mcpUserID := CreateTestUser(t, db, fmt.Sprintf("apim-mcp-%d", seq), "password123", "viewer")
		if _, err := db.Exec("UPDATE users SET must_change_password=1 WHERE id=?", mcpUserID); err != nil {
			t.Fatalf("APIMatrix: set must_change_password: %v", err)
		}
		token := createBearer(mcpUserID, []string{"accounts.manage"}, nil)
		scenarios = append(scenarios, scenario{
			name:     "bearer_must_change_pwd",
			expected: matrix.BearerMustChangePwd,
			setup: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer "+token)
			},
		})
	}

	for _, s := range scenarios {
		s := s // capture loop variable
		t.Run(s.name, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if body != "" {
				bodyReader = bytes.NewReader([]byte(body))
			} else {
				bodyReader = bytes.NewReader(nil)
			}

			req, err := http.NewRequest(method, ts.URL+path, bodyReader)
			if err != nil {
				t.Fatalf("create request: %v", err)
			}
			if body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			s.setup(req)

			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != s.expected {
				t.Errorf("scenario %s: expected status %d, got %d", s.name, s.expected, resp.StatusCode)
			}
		})
	}
}

// ParityCheck fetches the same resource via the HTML route (session cookie auth)
// and the API route (Bearer token auth), asserting both return HTTP 200.
// It logs the number of items returned in the API response's "data" field.
//
// htmlPath: e.g., "/accounts"
// apiPath:  e.g., "/api/v1/accounts"
func ParityCheck(t *testing.T, ts *httptest.Server, sessionCookie, csrfToken, bearerToken, htmlPath, apiPath string) {
	t.Helper()

	// Fetch HTML route.
	htmlReq, err := http.NewRequest(http.MethodGet, ts.URL+htmlPath, nil)
	if err != nil {
		t.Fatalf("ParityCheck: create html request: %v", err)
	}
	htmlReq.AddCookie(&http.Cookie{Name: "session_id", Value: sessionCookie})
	if csrfToken != "" {
		htmlReq.Header.Set("X-CSRF-Token", csrfToken)
	}
	htmlResp, err := ts.Client().Do(htmlReq)
	if err != nil {
		t.Fatalf("ParityCheck: html request: %v", err)
	}
	defer htmlResp.Body.Close()

	if htmlResp.StatusCode != http.StatusOK {
		t.Errorf("ParityCheck: html route %s: expected 200, got %d", htmlPath, htmlResp.StatusCode)
	}

	// Fetch API route.
	apiReq, err := http.NewRequest(http.MethodGet, ts.URL+apiPath, nil)
	if err != nil {
		t.Fatalf("ParityCheck: create api request: %v", err)
	}
	apiReq.Header.Set("Authorization", "Bearer "+bearerToken)
	apiResp, err := ts.Client().Do(apiReq)
	if err != nil {
		t.Fatalf("ParityCheck: api request: %v", err)
	}
	defer apiResp.Body.Close()

	if apiResp.StatusCode != http.StatusOK {
		t.Errorf("ParityCheck: api route %s: expected 200, got %d", apiPath, apiResp.StatusCode)
	}

	// Decode API response and report item count.
	var apiData map[string]any
	if err := json.NewDecoder(apiResp.Body).Decode(&apiData); err == nil {
		t.Logf("ParityCheck %s vs %s: API returned %d items", htmlPath, apiPath, countDataItems(apiData))
	}
}

// countDataItems counts the number of items in the "data" array of an API response envelope.
// Returns -1 if the key is absent or is not a slice.
func countDataItems(data map[string]any) int {
	if d, ok := data["data"]; ok {
		if arr, ok := d.([]any); ok {
			return len(arr)
		}
	}
	return -1
}

// contractMu guards contractChecks for concurrent test usage.
var contractMu sync.Mutex

// contractChecks accumulates recorded API responses for OpenAPI contract validation.
// Used by T9's TestOpenAPIContract to validate recorded responses against the spec.
var contractChecks []ContractCheck

// ContractCheck is a recorded API response for contract validation.
type ContractCheck struct {
	Method string
	Path   string
	Status int
	Body   []byte
}

// RecordContractCheck appends a response to the global contract check registry.
// Called by domain endpoint tests to register responses for T9 validation.
func RecordContractCheck(method, path string, status int, body []byte) {
	contractMu.Lock()
	defer contractMu.Unlock()
	contractChecks = append(contractChecks, ContractCheck{
		Method: method,
		Path:   path,
		Status: status,
		Body:   body,
	})
}

// GetContractChecks returns a snapshot of all recorded contract checks.
func GetContractChecks() []ContractCheck {
	contractMu.Lock()
	defer contractMu.Unlock()
	out := make([]ContractCheck, len(contractChecks))
	copy(out, contractChecks)
	return out
}

// ResetContractChecks clears the global contract check registry.
// Call at the start of contract-validation tests to ensure a clean slate.
func ResetContractChecks() {
	contractMu.Lock()
	defer contractMu.Unlock()
	contractChecks = contractChecks[:0]
}
