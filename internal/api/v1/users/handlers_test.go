package users_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/auth"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/api/v1/users"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &users.Handler{Access: access.New(db, auth.HashPassword)}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
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
	// Every scope, so the token can assign every non-admin role; a token never
	// counts as admin.
	_, tok, err := testutil.CreateAPIToken(db, adminID,
		fmt.Sprintf("test-users-%d", time.Now().UnixNano()),
		model.AllCapabilities, nil)
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

	t.Run("invalid id returns 400", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/users/notanumber", tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("forbidden returns 403", func(t *testing.T) {
		viewerID := testutil.CreateTestUser(t, db, "viewer-get-user", "pw", "viewer")
		_, noCapTok, err := testutil.CreateAPIToken(db, viewerID, "no-cap-get-user", []string{}, nil)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		resp := doReq(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/users/%d", adminID), noCapTok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
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
			"role":      "viewer",
			"password":  "pass1234",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})

	t.Run("forbidden returns 403", func(t *testing.T) {
		viewerID := testutil.CreateTestUser(t, db, "viewer-create-user", "pw", "viewer")
		_, noCapTok, err := testutil.CreateAPIToken(db, viewerID, "no-cap-create-user", []string{}, nil)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		body := map[string]any{
			"username":  "shouldnotcreate",
			"full_name": "Nope",
			"role":      "viewer",
			"password":  "pass1234",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", noCapTok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		body := map[string]any{
			"username":         "badbody",
			"full_name":        "Bad Body",
			"role":             "viewer",
			"password":         "pass1234",
			"unexpected_field": "boom",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("missing username returns 422", func(t *testing.T) {
		body := map[string]any{
			"full_name": "No Username",
			"role":      "viewer",
			"password":  "pass1234",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("missing full_name returns 422", func(t *testing.T) {
		body := map[string]any{
			"username": "nofullname",
			"role":     "viewer",
			"password": "pass1234",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid role returns 422", func(t *testing.T) {
		body := map[string]any{
			"username":  "badrole",
			"full_name": "Bad Role",
			"role":      "not-a-real-role",
			"password":  "pass1234",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("short password returns 422", func(t *testing.T) {
		body := map[string]any{
			"username":  "shortpwd",
			"full_name": "Short Password",
			"role":      "viewer",
			"password":  "short",
		}
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})
}

func TestUpdateUser(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("forbidden returns 403", func(t *testing.T) {
		targetID := testutil.CreateTestUser(t, db, "update-target-forbidden", "pw", "viewer")
		viewerID := testutil.CreateTestUser(t, db, "viewer-update-user", "pw", "viewer")
		_, noCapTok, err := testutil.CreateAPIToken(db, viewerID, "no-cap-update-user", []string{}, nil)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		body := map[string]any{"full_name": "Nope", "role": "viewer"}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", targetID), noCapTok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		body := map[string]any{"full_name": "X", "role": "viewer"}
		resp := doReq(t, ts, http.MethodPut, "/api/v1/users/notanumber", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("missing user returns 404", func(t *testing.T) {
		body := map[string]any{"full_name": "X", "role": "viewer"}
		resp := doReq(t, ts, http.MethodPut, "/api/v1/users/999999", tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("missing full_name returns 422", func(t *testing.T) {
		targetID := testutil.CreateTestUser(t, db, "update-target-nofn", "pw", "viewer")
		body := map[string]any{"role": "viewer"}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", targetID), tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid role returns 422", func(t *testing.T) {
		targetID := testutil.CreateTestUser(t, db, "update-target-badrole", "pw", "viewer")
		body := map[string]any{"full_name": "X", "role": "not-a-real-role"}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", targetID), tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("short password returns 422", func(t *testing.T) {
		targetID := testutil.CreateTestUser(t, db, "update-target-shortpw", "pw", "viewer")
		body := map[string]any{"full_name": "X", "role": "viewer", "password": "short"}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", targetID), tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("valid update returns 200 and persists changes", func(t *testing.T) {
		targetID := testutil.CreateTestUser(t, db, "update-target-ok", "pw", "viewer")
		body := map[string]any{
			"full_name": "Updated Name",
			"role":      "bookkeeper",
			"is_active": false,
		}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", targetID), tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			var errBody map[string]any
			json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
			t.Fatalf("expected 200, got %d: %v", resp.StatusCode, errBody)
		}
		var env struct {
			Data model.User `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.FullName != "Updated Name" || env.Data.Role != "bookkeeper" || env.Data.IsActive {
			t.Errorf("unexpected updated user: %+v", env.Data)
		}
	})

	t.Run("password change for other user forces must_change_password", func(t *testing.T) {
		targetID := testutil.CreateTestUser(t, db, "update-target-pwd", "pw", "viewer")
		body := map[string]any{
			"full_name": "Pwd Target",
			"role":      "viewer",
			"password":  "newpassword123",
		}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", targetID), tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		u, err := testutil.GetUserByID(db, targetID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if !u.MustChangePassword {
			t.Error("expected must_change_password to be true after admin changes another user's password")
		}
	})

	t.Run("self password change does not force must_change_password", func(t *testing.T) {
		// A non-admin user manager: a token can never manage an admin account.
		if err := testutil.CreateRole(db, &model.Role{Name: "usermgr", Capabilities: []string{model.CapUsersManage}}); err != nil {
			t.Fatal(err)
		}
		selfID := testutil.CreateTestUser(t, db, "update-self-pwd", "pw", "usermgr")
		_, selfTok, err := testutil.CreateAPIToken(db, selfID, "self-pwd-tok", []string{model.CapUsersManage}, nil)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		body := map[string]any{
			"full_name": "Self Pwd",
			"role":      "usermgr",
			"password":  "newpassword123",
		}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", selfID), selfTok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		u, err := testutil.GetUserByID(db, selfID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if u.MustChangePassword {
			t.Error("expected must_change_password to remain false for self password change")
		}
	})
}

func TestDeleteUser(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("forbidden returns 403", func(t *testing.T) {
		targetID := testutil.CreateTestUser(t, db, "delete-target-forbidden", "pw", "viewer")
		viewerID := testutil.CreateTestUser(t, db, "viewer-delete-user", "pw", "viewer")
		_, noCapTok, err := testutil.CreateAPIToken(db, viewerID, "no-cap-delete-user", []string{}, nil)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		resp := doReq(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", targetID), noCapTok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodDelete, "/api/v1/users/notanumber", tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("missing user returns 404", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodDelete, "/api/v1/users/999999", tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("valid delete deactivates user returns 204", func(t *testing.T) {
		targetID := testutil.CreateTestUser(t, db, "delete-target-ok", "pw", "viewer")
		resp := doReq(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", targetID), tok, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("expected 204, got %d", resp.StatusCode)
		}
		u, err := testutil.GetUserByID(db, targetID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if u.IsActive {
			t.Error("expected user to be deactivated")
		}
	})
}

func TestCapabilityEnforcement(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	viewerID := testutil.CreateTestUser(t, db, "viewer-users", "pw", "viewer")
	_, noCapTok, err := testutil.CreateAPIToken(db, viewerID, "no-cap-users", []string{}, nil)
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

	_, tok, err := testutil.CreateAPIToken(db, adminID,
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
			"full_name": "Admin",
			"role":      "admin",
			"is_active": isActive,
		}
		resp := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", adminID), tok, body)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}
	})
}

func TestUserManagerCannotEscalate(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	if err := testutil.CreateRole(db, &model.Role{Name: "hr", Capabilities: []string{model.CapUsersManage}}); err != nil {
		t.Fatal(err)
	}
	managerID := testutil.CreateTestUser(t, db, "manager", "pw", "hr")
	_, tok, err := testutil.CreateAPIToken(db, managerID, "manager-tok", []string{model.CapUsersManage}, nil)
	if err != nil {
		t.Fatal(err)
	}
	status := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		resp := doReq(t, ts, method, path, tok, body)
		defer resp.Body.Close()
		var env map[string]any
		json.NewDecoder(resp.Body).Decode(&env) //nolint:errcheck
		return resp.StatusCode, env
	}

	t.Run("create admin returns 422", func(t *testing.T) {
		code, env := status(http.MethodPost, "/api/v1/users", map[string]any{"username": "sneaky", "full_name": "S", "role": "admin", "password": "pass1234"})
		fields, _ := env["fields"].(map[string]any)
		if code != http.StatusUnprocessableEntity || fields["role"] != "outside your capabilities" {
			t.Errorf("expected 422 on role, got %d %v", code, env)
		}
	})

	t.Run("promote self returns 422", func(t *testing.T) {
		code, _ := status(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", managerID), map[string]any{"full_name": "M", "role": "admin", "is_active": true})
		if code != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", code)
		}
	})

	adminUnchanged := func(t *testing.T) {
		t.Helper()
		var role string
		var active bool
		if err := db.QueryRow(`SELECT role, is_active FROM users WHERE id=1`).Scan(&role, &active); err != nil {
			t.Fatal(err)
		}
		if role != model.RoleAdmin || !active {
			t.Errorf("admin changed: role=%s active=%v", role, active)
		}
	}

	t.Run("update admin returns 403", func(t *testing.T) {
		if code, _ := status(http.MethodPut, "/api/v1/users/1", map[string]any{"full_name": "Owned", "role": "hr", "is_active": true, "password": "owned1234"}); code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", code)
		}
		adminUnchanged(t)
	})

	t.Run("delete admin returns 403", func(t *testing.T) {
		if code, _ := status(http.MethodDelete, "/api/v1/users/1", nil); code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", code)
		}
		adminUnchanged(t)
	})
}

// An admin's token, even with every scope, cannot create admins or take over an
// admin account (e.g. reset its password without the current one).
func TestAdminTokenCannotManageAdmins(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)
	var hash string
	if err := db.QueryRow(`SELECT password FROM users WHERE id=1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}

	t.Run("create admin returns 422", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodPost, "/api/v1/users", tok, map[string]any{"username": "second-admin", "full_name": "A", "role": "admin", "password": "pass1234"})
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("expected 422, got %d", resp.StatusCode)
		}
	})

	t.Run("reset admin password returns 403", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodPut, "/api/v1/users/1", tok, map[string]any{"full_name": "Admin", "role": "admin", "is_active": true, "password": "owned1234"})
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
		var after string
		if err := db.QueryRow(`SELECT password FROM users WHERE id=1`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != hash {
			t.Error("admin password changed through a token")
		}
	})
}
