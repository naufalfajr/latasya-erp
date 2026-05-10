package auth_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	v1auth "github.com/naufal/latasya-erp/internal/api/v1/auth"
	"github.com/naufal/latasya-erp/internal/audit"
	authpkg "github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T) (*httptest.Server, *sql.DB, *v1auth.Handler) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := v1auth.New(db, true)

	mux := http.NewServeMux()
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("POST /api/v1/auth/logout", h.Logout)
	apiMux.HandleFunc("GET /api/v1/auth/me", h.Me)
	apiMux.HandleFunc("GET /api/v1/auth/csrf", h.CSRF)
	apiMux.HandleFunc("POST /api/v1/auth/password/change", h.PasswordChange)

	mux.Handle("/api/v1/", v1.BearerOrCookie(db)(apiMux))
	mux.Handle("POST /api/v1/auth/login", http.HandlerFunc(h.Login))

	srv := httptest.NewServer(audit.RequestContext(mux))
	t.Cleanup(srv.Close)
	return srv, db, h
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any, setup func(*http.Request)) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if setup != nil {
		setup(req)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func TestLogin_Success(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "alice", "password123", model.RoleBookkeeper)
	_ = userID

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "password123",
	}, nil)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}

	var got struct {
		Data struct {
			User      map[string]any `json:"user"`
			CSRFToken string         `json:"csrf_token"`
		} `json:"data"`
	}
	decodeBody(t, resp, &got)

	if got.Data.CSRFToken == "" {
		t.Errorf("csrf_token: got empty")
	}
	if got.Data.User["username"] != "alice" {
		t.Errorf("username: got %v, want alice", got.Data.User["username"])
	}

	var sawCookie bool
	for _, c := range resp.Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			sawCookie = true
			if !c.HttpOnly {
				t.Errorf("session cookie should be HttpOnly")
			}
			if c.MaxAge <= 0 {
				t.Errorf("session cookie MaxAge should be positive, got %d", c.MaxAge)
			}
		}
	}
	if !sawCookie {
		t.Errorf("expected session_id cookie set")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	srv, db, _ := newTestServer(t)
	testutil.CreateTestUser(t, db, "bob", "correct-pw", model.RoleBookkeeper)

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "bob",
		"password": "wrong-pw",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", resp.StatusCode)
	}
	var env v1.ErrorEnvelope
	decodeBody(t, resp, &env)
	if env.Code != "invalid_credentials" {
		t.Errorf("code: got %q, want invalid_credentials", env.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "ghost",
		"password": "whatever",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", resp.StatusCode)
	}
	var env v1.ErrorEnvelope
	decodeBody(t, resp, &env)
	if env.Code != "invalid_credentials" {
		t.Errorf("code: got %q, want invalid_credentials", env.Code)
	}
}

func TestLogin_InactiveUser(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "disabled", "password123", model.RoleBookkeeper)
	if _, err := db.Exec("UPDATE users SET is_active=0 WHERE id=?", userID); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "disabled",
		"password": "password123",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", resp.StatusCode)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	srv, _, _ := newTestServer(t)

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "",
		"password": "",
	}, nil)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want 422", resp.StatusCode)
	}
}

func TestLogout_CookieSession(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "carol", "pw", model.RoleBookkeeper)
	sessionID := testutil.CreateTestSession(t, db, userID)

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/logout", nil, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	})

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	resp.Body.Close()

	if _, err := authpkg.GetSessionUserID(db, sessionID); err == nil {
		t.Errorf("expected session to be deleted")
	}

	var clearedCookieSeen bool
	for _, c := range resp.Cookies() {
		if c.Name == "session_id" && c.MaxAge < 0 {
			clearedCookieSeen = true
		}
	}
	if !clearedCookieSeen {
		t.Errorf("expected session_id cookie cleared with MaxAge<0")
	}
}

func TestMe_Cookie(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "dora", "pw", model.RoleBookkeeper)
	sessionID := testutil.CreateTestSession(t, db, userID)

	resp := doJSON(t, srv, http.MethodGet, "/api/v1/auth/me", nil, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var got struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Data["username"] != "dora" {
		t.Errorf("username: got %v, want dora", got.Data["username"])
	}
	if got.Data["auth_method"] != "cookie" {
		t.Errorf("auth_method: got %v, want cookie", got.Data["auth_method"])
	}
	if got.Data["token_id"] != nil {
		t.Errorf("token_id: got %v, want nil", got.Data["token_id"])
	}
}

func TestMe_Bearer(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "eve", "pw", model.RoleBookkeeper)
	tok, plaintext, err := model.CreateAPIToken(db, userID, "t", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resp := doJSON(t, srv, http.MethodGet, "/api/v1/auth/me", nil, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+plaintext)
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var got struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Data["auth_method"] != "bearer" {
		t.Errorf("auth_method: got %v, want bearer", got.Data["auth_method"])
	}
	tid, ok := got.Data["token_id"].(float64)
	if !ok || int(tid) != tok.ID {
		t.Errorf("token_id: got %v, want %d", got.Data["token_id"], tok.ID)
	}
}

func TestCSRF_Cookie(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "frank", "pw", model.RoleBookkeeper)
	sessionID := testutil.CreateTestSession(t, db, userID)

	resp := doJSON(t, srv, http.MethodGet, "/api/v1/auth/csrf", nil, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var got struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CSRFToken == "" {
		t.Errorf("csrf_token: got empty")
	}
}

func TestCSRF_Bearer(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "gina", "pw", model.RoleBookkeeper)
	_, plaintext, err := model.CreateAPIToken(db, userID, "t", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resp := doJSON(t, srv, http.MethodGet, "/api/v1/auth/csrf", nil, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+plaintext)
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestPasswordChange_Success(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "harry", "old-password", model.RoleBookkeeper)
	sessionID := testutil.CreateTestSession(t, db, userID)

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/password/change", map[string]string{
		"current_password": "old-password",
		"new_password":     "new-password-456",
		"confirm_password": "new-password-456",
	}, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}

	user, err := model.GetUserByID(db, userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !authpkg.CheckPassword(user.Password, "new-password-456") {
		t.Errorf("password not updated")
	}
	if user.MustChangePassword {
		t.Errorf("must_change_password should be cleared")
	}
}

func TestPasswordChange_WrongCurrent(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "irene", "actual-pw", model.RoleBookkeeper)
	sessionID := testutil.CreateTestSession(t, db, userID)

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/password/change", map[string]string{
		"current_password": "guess",
		"new_password":     "new-password-789",
		"confirm_password": "new-password-789",
	}, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestPasswordChange_TooShort(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "jack", "old-password", model.RoleBookkeeper)
	sessionID := testutil.CreateTestSession(t, db, userID)

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/password/change", map[string]string{
		"current_password": "old-password",
		"new_password":     "short",
		"confirm_password": "short",
	}, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want 422", resp.StatusCode)
	}
	var env v1.ErrorEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(strings.ToLower(env.Fields["new_password"]), "8 characters") {
		t.Errorf("expected new_password length error, got %v", env.Fields)
	}
}

func TestPasswordChange_BearerNotBlockedByMustChange(t *testing.T) {
	srv, db, _ := newTestServer(t)
	userID := testutil.CreateTestUser(t, db, "kate", "old-password", model.RoleBookkeeper)
	if err := model.SetMustChangePassword(db, userID, true); err != nil {
		t.Fatalf("SetMustChangePassword: %v", err)
	}
	_, plaintext, err := model.CreateAPIToken(db, userID, "t", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resp := doJSON(t, srv, http.MethodPost, "/api/v1/auth/password/change", map[string]string{
		"current_password": "old-password",
		"new_password":     "new-password-987",
		"confirm_password": "new-password-987",
	}, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+plaintext)
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("bearer password change blocked: status=%d body=%s", resp.StatusCode, body)
	}
}
