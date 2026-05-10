package roles_test

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
	"github.com/naufal/latasya-erp/internal/api/v1/roles"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &roles.Handler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/roles", h.List)
	mux.HandleFunc("GET /api/v1/roles/capabilities", h.Capabilities)
	mux.HandleFunc("GET /api/v1/roles/{name}", h.Get)
	mux.HandleFunc("POST /api/v1/roles", h.Create)
	mux.HandleFunc("PUT /api/v1/roles/{name}", h.Update)
	mux.HandleFunc("DELETE /api/v1/roles/{name}", h.Delete)
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
		fmt.Sprintf("test-roles-%d", time.Now().UnixNano()),
		[]string{model.CapRolesManage}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return tok
}

func doReq(t *testing.T, ts *httptest.Server, method, path, bearer string, body any) *http.Response {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

func TestListRoles(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/roles", "", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("admin returns 200 with list", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/roles", tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var env struct {
			Data []model.Role `json:"data"`
			Meta v1.Meta      `json:"meta"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(env.Data) == 0 {
			t.Error("expected at least one role")
		}
	})
}

func TestGetCapabilities(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	viewerID := testutil.CreateTestUser(t, db, "viewer-caps", "pw", "viewer")
	_, viewerTok, err := model.CreateAPIToken(db, viewerID, "viewer-caps-tok", []string{}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	resp := doReq(t, ts, http.MethodGet, "/api/v1/roles/capabilities", viewerTok, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var env struct {
		Data []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data) == 0 {
		t.Error("expected at least one capability")
	}
}

func TestCreateRole(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("valid role creates 201", func(t *testing.T) {
		body := map[string]any{
			"name":         "testrole1",
			"description":  "Test role",
			"capabilities": []string{model.CapReportsView},
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/roles", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			var errBody map[string]any
			json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
			t.Fatalf("expected 201, got %d: %v", resp.StatusCode, errBody)
		}
		var env struct {
			Data model.Role `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.Name != "testrole1" {
			t.Errorf("expected name testrole1, got %s", env.Data.Name)
		}
	})

	t.Run("duplicate name returns 422", func(t *testing.T) {
		body := map[string]any{"name": "admin", "capabilities": []string{}}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/roles", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})
}

func TestCapabilityEnforcement(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	viewerID := testutil.CreateTestUser(t, db, "viewer-roles", "pw", "viewer")
	_, noCapTok, err := model.CreateAPIToken(db, viewerID, "no-cap-roles", []string{}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	resp := doReq(t, ts, http.MethodGet, "/api/v1/roles", noCapTok, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAdminRoleProtection(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("cannot edit admin role returns 409", func(t *testing.T) {
		body := map[string]any{
			"description":  "Hacked",
			"capabilities": []string{},
		}
		resp := doReq(t, ts, http.MethodPut, "/api/v1/roles/admin", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
		var errEnv v1.ErrorEnvelope
		json.NewDecoder(resp.Body).Decode(&errEnv) //nolint:errcheck
		if errEnv.Code != v1.CodeConflict {
			t.Errorf("expected conflict code, got %s", errEnv.Code)
		}
	})

	t.Run("cannot delete admin role returns 409", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodDelete, "/api/v1/roles/admin", tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})

	t.Run("cannot delete role with users returns 409", func(t *testing.T) {
		if err := model.CreateRole(db, &model.Role{Name: "inuse", Capabilities: []string{}}); err != nil {
			t.Fatalf("create role: %v", err)
		}
		testutil.CreateTestUser(t, db, "userinuse", "pw", "inuse")
		resp := doReq(t, ts, http.MethodDelete, "/api/v1/roles/inuse", tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})
}
