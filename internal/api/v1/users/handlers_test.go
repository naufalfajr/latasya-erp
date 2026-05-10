package users_test

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
	"github.com/naufal/latasya-erp/internal/api/v1/users"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &users.Handler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/users", h.List)
	mux.HandleFunc("GET /api/v1/users/{id}", h.Get)
	mux.HandleFunc("POST /api/v1/users", h.Create)
	mux.HandleFunc("PUT /api/v1/users/{id}", h.Update)
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.Delete)
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
		fmt.Sprintf("test-users-%d", time.Now().UnixNano()),
		[]string{model.CapUsersManage}, nil)
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

func TestListUsers(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/users", "", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("authenticated admin returns 200 with list", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/users", tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var env struct {
			Data []model.User `json:"data"`
			Meta v1.Meta      `json:"meta"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(env.Data) == 0 {
			t.Error("expected at least one user")
		}
		for _, u := range env.Data {
			if u.Password != "" {
				t.Errorf("password must not appear in response, user %s has non-empty password field", u.Username)
			}
		}
	})
}

func TestGetUser(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("get admin id: %v", err)
	}

	t.Run("existing user returns 200", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", adminID), tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var env struct {
			Data model.User `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.ID != adminID {
			t.Errorf("expected id %d, got %d", adminID, env.Data.ID)
		}
		if env.Data.Password != "" {
			t.Error("password must not appear in response")
		}
	})

	t.Run("missing user returns 404", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/users/999999", tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

func TestCreateUser(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("valid input creates user", func(t *testing.T) {
		body := map[string]any{
			"username":  "newuser1",
			"full_name": "New User One",
			"role":      "viewer",
			"password":  "pass1234",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			var errBody map[string]any
			json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
			t.Fatalf("expected 201, got %d: %v", resp.StatusCode, errBody)
		}
		var env struct {
			Data model.User `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.Username != "newuser1" {
			t.Errorf("expected username newuser1, got %s", env.Data.Username)
		}
		if !env.Data.MustChangePassword {
			t.Error("expected must_change_password=true for new user")
		}
		if env.Data.Password != "" {
			t.Error("password must not appear in response")
		}
	})

	t.Run("missing password returns 422", func(t *testing.T) {
		body := map[string]any{
			"username":  "nopwd",
			"full_name": "No Password",
			"role":      "viewer",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("duplicate username returns 409", func(t *testing.T) {
		body := map[string]any{
			"username":  "admin",
			"full_name": "Admin Dup",
			"role":      "admin",
			"password":  "pass1234",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})
}

func TestCapabilityEnforcement(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	viewerID := testutil.CreateTestUser(t, db, "viewer-users", "pw", "viewer")
	_, noCapTok, err := model.CreateAPIToken(db, viewerID, "no-cap-users", []string{}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	resp := doReq(t, ts, http.MethodGet, "/api/v1/users", noCapTok, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestSelfProtection(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("get admin id: %v", err)
	}

	_, tok, err := model.CreateAPIToken(db, adminID,
		fmt.Sprintf("admin-self-%d", time.Now().UnixNano()),
		[]string{model.CapUsersManage}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	t.Run("cannot deactivate self via DELETE returns 409", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", adminID), tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})

	t.Run("cannot deactivate self via PUT returns 409", func(t *testing.T) {
		isActive := false
		body := map[string]any{
			"full_name":  "Admin",
			"role":       "admin",
			"is_active":  isActive,
		}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", adminID), tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})
}
