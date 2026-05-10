package auditapi_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	auditapi "github.com/naufal/latasya-erp/internal/api/v1/audit"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &auditapi.Handler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/audit", h.List)
	ts := httptest.NewServer(v1.BearerOrCookie(db)(mux))
	t.Cleanup(ts.Close)
	return ts
}

func adminToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("get admin: %v", err)
	}
	_, tok, err := model.CreateAPIToken(db, adminID,
		fmt.Sprintf("test-audit-%d", time.Now().UnixNano()),
		[]string{model.CapAuditView}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return tok
}

func doReq(t *testing.T, ts *httptest.Server, method, path, bearer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestListAudit(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/audit", "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("authenticated admin returns 200", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/audit", tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var env struct {
			Data []any   `json:"data"`
			Meta v1.Meta `json:"meta"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data == nil {
			t.Error("expected non-nil data array")
		}
		if env.Meta.PerPage <= 0 {
			t.Error("expected meta.per_page > 0")
		}
	})

	t.Run("filter by actor query param", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/audit?actor=admin", tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("filter by date range", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/audit?from=2020-01-01&to=2030-12-31", tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestCapabilityEnforcement(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	viewerID := testutil.CreateTestUser(t, db, "viewer-audit", "pw", "viewer")
	_, noCapTok, err := model.CreateAPIToken(db, viewerID, "no-cap-audit", []string{}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	resp := doReq(t, ts, http.MethodGet, "/api/v1/audit", noCapTok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}
