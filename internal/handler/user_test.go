package handler_test

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
)

// mustUser inserts a user directly and returns its ID.
func mustUser(t *testing.T, db *sql.DB, username, role string) int {
	t.Helper()
	hash, err := auth.HashPassword("initial-pass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := db.Exec(
		"INSERT INTO users (username, password, full_name, role, is_active) VALUES (?, ?, ?, ?, 1)",
		username, hash, "Test "+username, role,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var id int
	if err := db.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&id); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	return id
}

func TestNewUser_RendersForm(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/users/new", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "New User") {
		t.Error("expected 'New User' heading")
	}
}

func TestEditUser_RendersForm(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustUser(t, db, "editme", "viewer")

	req, _ := requestWithCookies(db, "GET", ts.URL+"/users/"+strconv.Itoa(id)+"/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "editme") {
		t.Error("expected the username in the edit form")
	}
}

func TestEditUser_InvalidID_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/users/not-a-number/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-numeric id, got %d", resp.StatusCode)
	}
}

func TestEditUser_UnknownID_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/users/999999/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown id, got %d", resp.StatusCode)
	}
}

func TestUpdateUser_InvalidID_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := "full_name=X&role=viewer&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users/not-a-number", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-numeric id, got %d", resp.StatusCode)
	}
}

func TestUpdateUser_UnknownID_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := "full_name=X&role=viewer&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users/999999", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown id, got %d", resp.StatusCode)
	}
}

func TestUpdateUser_ValidationError_EmptyFullName(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustUser(t, db, "blankname", "viewer")

	form := "full_name=&role=viewer&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users/"+strconv.Itoa(id), cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Full name is required") {
		t.Error("expected 'Full name is required' error in body")
	}
}

func TestUpdateUser_ValidationError_InvalidRole(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustUser(t, db, "badrole", "viewer")

	form := "full_name=Bad+Role&role=not-a-role&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users/"+strconv.Itoa(id), cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Invalid role") {
		t.Error("expected 'Invalid role' error in body")
	}
}

func TestUpdateUser_ValidationError_ShortPassword(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustUser(t, db, "shortpass", "viewer")

	form := "full_name=Short+Pass&role=viewer&is_active=on&password=abc"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users/"+strconv.Itoa(id), cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Password must be at least 4 characters") {
		t.Error("expected password-length error in body")
	}
}

// The handler forces IsActive back to true when an admin tries to
// deactivate their own account via the edit form (is_active box unchecked).
func TestUpdateUser_CannotDeactivateSelf(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// is_active omitted entirely == unchecked checkbox.
	form := "full_name=Administrator&role=admin"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users/1", cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	var active bool
	if err := db.QueryRow("SELECT is_active FROM users WHERE id = 1").Scan(&active); err != nil {
		t.Fatalf("query admin: %v", err)
	}
	if !active {
		t.Error("admin should not be able to deactivate their own account")
	}
}

// Resetting another user's password should force must_change_password=1;
// resetting one's own should not.
func TestUpdateUser_PasswordReset_ForcesChangeForOtherUser(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustUser(t, db, "resetme", "viewer")

	form := "full_name=Reset+Me&role=viewer&is_active=on&password=newpassword123"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users/"+strconv.Itoa(id), cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	var mustChange bool
	if err := db.QueryRow("SELECT must_change_password FROM users WHERE id = ?", id).Scan(&mustChange); err != nil {
		t.Fatalf("query user: %v", err)
	}
	if !mustChange {
		t.Error("resetting another user's password should force must_change_password")
	}
}

func TestUpdateUser_PasswordReset_SelfNotForced(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := "full_name=Administrator&role=admin&is_active=on&password=anothernewpass1"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users/1", cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	var mustChange bool
	if err := db.QueryRow("SELECT must_change_password FROM users WHERE id = 1").Scan(&mustChange); err != nil {
		t.Fatalf("query admin: %v", err)
	}
	if mustChange {
		t.Error("admin resetting their own password should not force a re-change")
	}
}

func TestDeleteUser_InvalidID_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/users/not-a-number", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-numeric id, got %d", resp.StatusCode)
	}
}

func TestDeleteUser_UnknownID_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/users/999999", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown id, got %d", resp.StatusCode)
	}
}

func TestDeleteUser_CannotDeleteSelf(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/users/1", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	var active bool
	if err := db.QueryRow("SELECT is_active FROM users WHERE id = 1").Scan(&active); err != nil {
		t.Fatalf("query admin: %v", err)
	}
	if !active {
		t.Error("admin should not be able to delete/deactivate their own account")
	}
}

func TestDeleteUser_HTMX_Deactivates(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustUser(t, db, "deactivateme", "viewer")

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/users/"+strconv.Itoa(id), cookies, "")
	req.Header.Set("HX-Request", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for HTMX delete, got %d", resp.StatusCode)
	}

	var active bool
	if err := db.QueryRow("SELECT is_active FROM users WHERE id = ?", id).Scan(&active); err != nil {
		t.Fatalf("query user: %v", err)
	}
	if active {
		t.Error("user should have been deactivated")
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	mustUser(t, db, "dupeuser", "viewer")

	form := "username=dupeuser&full_name=Dupe+User&password=test1234&role=viewer&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render on duplicate username), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "already exists") {
		t.Error("expected 'already exists' error in body")
	}
}
