package handler_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// latestAuditRow fetches the most recent audit_log row for a given action.
// Returns the matching row's key fields, or zero values if no row matched.
type auditRow struct {
	Action      string
	Actor       string
	TargetType  string
	TargetID    sql.NullInt64
	TargetLabel string
	Result      string
	Metadata    string
}

func latestAuditFor(t *testing.T, db *sql.DB, action string) auditRow {
	t.Helper()
	var r auditRow
	err := db.QueryRow(`
		SELECT action, COALESCE(actor_username, ''), COALESCE(target_type, ''),
		       target_id, COALESCE(target_label, ''), result, COALESCE(metadata, '')
		FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`, action).
		Scan(&r.Action, &r.Actor, &r.TargetType, &r.TargetID, &r.TargetLabel, &r.Result, &r.Metadata)
	if err != nil {
		t.Fatalf("no audit row for action %q: %v", action, err)
	}
	return r
}

// --- Login / Logout / Password change ---

func TestAudit_LoginSuccess(t *testing.T) {
	ts, db := testServer(t)
	_ = loginAsAdmin(t, ts)

	r := latestAuditFor(t, db, "auth.login")
	if r.Actor != "admin" {
		t.Errorf("actor = %q, want admin", r.Actor)
	}
	if r.Result != "ok" {
		t.Errorf("result = %q, want ok", r.Result)
	}
	if r.TargetType != "user" || r.TargetLabel != "admin" {
		t.Errorf("target = %s/%s, want user/admin", r.TargetType, r.TargetLabel)
	}
}

func TestAudit_LoginFailedUnknownUser(t *testing.T) {
	ts, db := testServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{"username": {"nobody"}, "password": {"whatever"}}
	resp, err := client.PostForm(ts.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	r := latestAuditFor(t, db, "auth.login_failed")
	if r.Actor != "nobody" {
		t.Errorf("actor = %q, want nobody", r.Actor)
	}
	if r.Result != "fail" {
		t.Errorf("result = %q, want fail", r.Result)
	}
	if !strings.Contains(r.Metadata, "unknown_user") {
		t.Errorf("metadata should contain reason=unknown_user, got %q", r.Metadata)
	}
}

func TestAudit_LoginFailedBadPassword(t *testing.T) {
	ts, db := testServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{"username": {"admin"}, "password": {"totally-wrong"}}
	resp, err := client.PostForm(ts.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	r := latestAuditFor(t, db, "auth.login_failed")
	if r.Actor != "admin" {
		t.Errorf("actor = %q, want admin", r.Actor)
	}
	if !strings.Contains(r.Metadata, "bad_password") {
		t.Errorf("metadata should contain reason=bad_password, got %q", r.Metadata)
	}
}

func TestAudit_Logout(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/logout", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	r := latestAuditFor(t, db, "auth.logout")
	if r.Actor != "admin" {
		t.Errorf("actor = %q, want admin", r.Actor)
	}
}

func TestAudit_PasswordChange(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{
		"current_password": {adminTestPassword},
		"new_password":     {"NewPass12345"},
		"confirm_password": {"NewPass12345"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/password/change", cookies, form.Encode())
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	r := latestAuditFor(t, db, "auth.password_change")
	if r.Actor != "admin" {
		t.Errorf("actor = %q, want admin", r.Actor)
	}
	if r.TargetType != "user" || r.TargetLabel != "admin" {
		t.Errorf("target = %s/%s, want user/admin", r.TargetType, r.TargetLabel)
	}
}

// --- User CRUD ---

func TestAudit_UserCreate(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{
		"username":  {"newuser1"},
		"full_name": {"New User"},
		"password":  {"password123"},
		"role":      {"viewer"},
		"is_active": {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, form.Encode())
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	r := latestAuditFor(t, db, "user.create")
	if r.Actor != "admin" {
		t.Errorf("actor = %q, want admin", r.Actor)
	}
	if r.TargetLabel != "newuser1" {
		t.Errorf("target_label = %q, want newuser1", r.TargetLabel)
	}
	if !strings.Contains(r.Metadata, `"after"`) {
		t.Errorf("metadata should contain 'after', got %q", r.Metadata)
	}
}

func TestAudit_UserUpdate_DiffsChangedFields(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Create a user to edit.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := url.Values{
		"username":  {"editme"},
		"full_name": {"Original"},
		"password":  {"password123"},
		"role":      {"viewer"},
		"is_active": {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, createForm.Encode())
	resp, _ := noRedirect.Do(req)
	resp.Body.Close()

	var id int
	db.QueryRow("SELECT id FROM users WHERE username = 'editme'").Scan(&id)

	// Update only full_name.
	updateForm := url.Values{
		"full_name": {"Renamed"},
		"role":      {"viewer"},
		"is_active": {"on"},
	}
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/users/"+strconv.Itoa(id), cookies, updateForm.Encode())
	resp2, _ := noRedirect.Do(req2)
	resp2.Body.Close()

	r := latestAuditFor(t, db, "user.update")
	if r.TargetLabel != "editme" {
		t.Errorf("target_label = %q, want editme", r.TargetLabel)
	}
	// full_name changed; role/is_active unchanged, so they should NOT appear in diff.
	if !strings.Contains(r.Metadata, "Original") || !strings.Contains(r.Metadata, "Renamed") {
		t.Errorf("metadata should contain both old/new full_name, got %q", r.Metadata)
	}
	if strings.Contains(r.Metadata, `"role"`) {
		t.Errorf("metadata should NOT include unchanged 'role' field, got %q", r.Metadata)
	}
}

func TestAudit_UserDelete(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := url.Values{
		"username":  {"deleteme"},
		"full_name": {"Delete Me"},
		"password":  {"password123"},
		"role":      {"viewer"},
		"is_active": {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, createForm.Encode())
	resp, _ := noRedirect.Do(req)
	resp.Body.Close()

	var id int
	db.QueryRow("SELECT id FROM users WHERE username = 'deleteme'").Scan(&id)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/users/"+strconv.Itoa(id), cookies, "")
	resp2, _ := noRedirect.Do(req2)
	resp2.Body.Close()

	r := latestAuditFor(t, db, "user.delete")
	if r.TargetLabel != "deleteme" {
		t.Errorf("target_label = %q, want deleteme", r.TargetLabel)
	}
	if !strings.Contains(r.Metadata, "is_active") {
		t.Errorf("metadata should describe is_active flip, got %q", r.Metadata)
	}
}

// --- Role CRUD ---

func TestAudit_RoleCreate(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{
		"name":         {"newrole"},
		"description":  {"New role"},
		"capabilities": {"reports.view"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, form.Encode())
	resp, _ := noRedirect.Do(req)
	resp.Body.Close()

	r := latestAuditFor(t, db, "role.create")
	if r.TargetLabel != "newrole" {
		t.Errorf("target_label = %q, want newrole", r.TargetLabel)
	}
	if !strings.Contains(r.Metadata, "reports.view") {
		t.Errorf("metadata should include capability, got %q", r.Metadata)
	}
}

func TestAudit_RoleUpdate_CapabilityDiff(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	// Narrow bookkeeper to reports.view only.
	form := url.Values{
		"description":  {"narrowed"},
		"capabilities": {"reports.view"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles/bookkeeper", cookies, form.Encode())
	resp, _ := noRedirect.Do(req)
	resp.Body.Close()

	r := latestAuditFor(t, db, "role.update")
	if r.TargetLabel != "bookkeeper" {
		t.Errorf("target_label = %q, want bookkeeper", r.TargetLabel)
	}
	// Both "before" and "after" must be present with a capability delta.
	if !strings.Contains(r.Metadata, "capabilities") {
		t.Errorf("metadata should include capabilities diff, got %q", r.Metadata)
	}
}

func TestAudit_RoleDelete(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	// Create then delete a custom role (system roles can't be deleted).
	createForm := url.Values{"name": {"doomed"}, "description": {"x"}, "capabilities": {"reports.view"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, createForm.Encode())
	resp, _ := noRedirect.Do(req)
	resp.Body.Close()

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/roles/doomed", cookies, "")
	resp2, _ := noRedirect.Do(req2)
	resp2.Body.Close()

	r := latestAuditFor(t, db, "role.delete")
	if r.TargetLabel != "doomed" {
		t.Errorf("target_label = %q, want doomed", r.TargetLabel)
	}
}

