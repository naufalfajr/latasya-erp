package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// loginAsUserManager logs in "manager", whose role ("manager-role") holds only
// users.manage, and returns the session cookies and the manager's user ID.
func loginAsUserManager(t *testing.T, ts *httptest.Server, db *sql.DB) ([]*http.Cookie, int) {
	t.Helper()
	cookies := loginAsCapabilityUser(t, ts, db, "manager", model.CapUsersManage)
	manager, err := testutil.GetUserByUsername(db, "manager")
	if err != nil {
		t.Fatalf("lookup manager: %v", err)
	}
	return cookies, manager.ID
}

func TestNewUser_ManagerOffersOnlyAssignableRoles(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	manager, _ := loginAsUserManager(t, ts, db)

	_, body := sendRequest(t, db, manager, "GET", ts.URL+"/users/new", "")
	if !strings.Contains(body, `value="manager-role"`) {
		t.Error("manager should be offered their own role")
	}
	for _, role := range []string{"admin", "bookkeeper", "viewer"} {
		if strings.Contains(body, `value="`+role+`"`) {
			t.Errorf("manager offered %q, which carries permissions they lack", role)
		}
	}
}

func TestNewUser_AdminOffersAllRoles(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	_, body := sendRequest(t, db, loginAsAdmin(t, ts), "GET", ts.URL+"/users/new", "")
	for _, role := range []string{"admin", "bookkeeper", "viewer"} {
		if !strings.Contains(body, `value="`+role+`"`) {
			t.Errorf("admin not offered %q", role)
		}
	}
}

func TestCreateUser_RoleOutsideOwnPermissions(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	manager, _ := loginAsUserManager(t, ts, db)

	resp, body := sendRequest(t, db, manager, "POST", ts.URL+"/users", "username=sneaky&full_name=Sneaky&role=admin&is_active=on&password=pass1234")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "You can only assign roles within your own permissions") {
		t.Errorf("expected the form with a role error, got %d", resp.StatusCode)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username='sneaky'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("admin user created by a non-admin manager")
	}
}

func TestCreateUser_EmptyRole_ShowsInvalidRole(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	_, body := sendRequest(t, db, loginAsAdmin(t, ts), "POST", ts.URL+"/users", "username=norole&full_name=No%20Role&role=&is_active=on&password=pass1234")
	if !strings.Contains(body, "Invalid role") || strings.Contains(body, "within your own permissions") {
		t.Error("an empty role should read as an invalid role, not a permissions problem")
	}
}

func TestUpdateUser_ManagerCannotPromoteSelf(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	manager, id := loginAsUserManager(t, ts, db)

	resp, body := sendRequest(t, db, manager, "POST", fmt.Sprintf("%s/users/%d", ts.URL, id), "full_name=Manager&role=admin&is_active=on")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "You can only assign roles within your own permissions") {
		t.Errorf("expected the form with a role error, got %d", resp.StatusCode)
	}
	var role string
	if err := db.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != "manager-role" {
		t.Errorf("manager role is now %q", role)
	}
}

func TestEditUser_ManagerForbiddenOnAdmin(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	manager, _ := loginAsUserManager(t, ts, db)
	if resp, _ := sendRequest(t, db, manager, "GET", ts.URL+"/users/1/edit", ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

// Refused before validation, so even an invalid form gets 403, not a re-rendered form.
func TestUpdateUser_ManagerForbiddenOnAdmin(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	manager, _ := loginAsUserManager(t, ts, db)
	var hash string
	if err := db.QueryRow(`SELECT password FROM users WHERE id=1`).Scan(&hash); err != nil {
		t.Fatal(err)
	}

	for _, form := range []string{"full_name=Owned&role=manager-role&is_active=on&password=owned1234", "full_name=&role=manager-role&is_active=on"} {
		if resp, _ := sendRequest(t, db, manager, "POST", ts.URL+"/users/1", form); resp.StatusCode != http.StatusForbidden {
			t.Errorf("POST %q: expected 403, got %d", form, resp.StatusCode)
		}
	}
	var after, role string
	if err := db.QueryRow(`SELECT password, role FROM users WHERE id=1`).Scan(&after, &role); err != nil {
		t.Fatal(err)
	}
	if after != hash || role != model.RoleAdmin {
		t.Error("admin password or role changed by a non-admin manager")
	}
}

func TestDeleteUser_ManagerCannotDeactivateAdmin(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	manager, _ := loginAsUserManager(t, ts, db)

	resp, _ := sendRequest(t, db, manager, "DELETE", ts.URL+"/users/1", "")
	if resp.StatusCode != http.StatusSeeOther || flashOf(resp) != "Cannot delete a user with more permissions than you" {
		t.Errorf("expected 303 with a refusal flash, got %d %q", resp.StatusCode, flashOf(resp))
	}
	var active bool
	if err := db.QueryRow(`SELECT is_active FROM users WHERE id=1`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Error("admin deactivated by a non-admin manager")
	}
}

// Admins skip the role lookup, so a user whose role row is gone stays fixable.
func TestEditUser_AdminCanFixUserWithMissingRole(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	admin := loginAsAdmin(t, ts)
	id := mustUser(t, db, "ghost", "ghostrole")

	if resp, _ := sendRequest(t, db, admin, "GET", fmt.Sprintf("%s/users/%d/edit", ts.URL, id), ""); resp.StatusCode != http.StatusOK {
		t.Errorf("edit form: expected 200, got %d", resp.StatusCode)
	}
	if resp, _ := sendRequest(t, db, admin, "POST", fmt.Sprintf("%s/users/%d", ts.URL, id), "full_name=Ghost&role=viewer&is_active=on"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("update: expected 303, got %d", resp.StatusCode)
	}
	var role string
	if err := db.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != model.RoleViewer {
		t.Errorf("role is %q, want viewer", role)
	}
}

func TestListUsers_ManagerSeesActionsOnlyOnManageableRows(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	manager, id := loginAsUserManager(t, ts, db)

	_, body := sendRequest(t, db, manager, "GET", ts.URL+"/users", "")
	if !strings.Contains(body, fmt.Sprintf(`href="/users/%d/edit"`, id)) {
		t.Error("manager should be able to edit their own row")
	}
	if strings.Contains(body, `href="/users/1/edit"`) || strings.Contains(body, `hx-delete="/users/1"`) {
		t.Error("manager is shown Edit/Deactivate on the admin's row")
	}
}

func TestListUsers_AdminSeesActionsOnEveryRow(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	id := mustUser(t, db, "someone", "viewer")
	_, body := sendRequest(t, db, loginAsAdmin(t, ts), "GET", ts.URL+"/users", "")
	if !strings.Contains(body, `href="/users/1/edit"`) || !strings.Contains(body, fmt.Sprintf(`href="/users/%d/edit"`, id)) {
		t.Error("admin should see Edit on every row")
	}
}

// A failure loading roles is a 500, not a page that silently hides every action.
// Called directly: the auth middleware itself needs the roles table.
func TestListUsers_RolesLoadError(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)
	if _, err := db.Exec(`ALTER TABLE roles RENAME TO roles_gone`); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin}
	for name, handle := range map[string]http.HandlerFunc{"users list": h.ListUsers, "new user form": h.NewUser} {
		rec := httptest.NewRecorder()
		handle(rec, httptest.NewRequest("GET", "/users", nil).WithContext(auth.WithUser(context.Background(), admin)))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: expected 500, got %d", name, rec.Code)
		}
	}

	// A non-admin's edit form can't check the target's role either.
	manager := &model.User{ID: 2, Username: "manager", Role: "manager-role", Capabilities: []string{model.CapUsersManage}}
	req := httptest.NewRequest("GET", "/users/1/edit", nil).WithContext(auth.WithUser(context.Background(), manager))
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	h.EditUser(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("manager edit form: expected 500, got %d", rec.Code)
	}
}
