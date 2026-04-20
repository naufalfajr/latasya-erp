package handler_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestListRoles_Admin(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/roles", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestListRoles_ViewerDenied(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/roles", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer should get 403 on /roles, got %d", resp.StatusCode)
	}
}

func TestCreateRole_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{
		"name":         {"manager"},
		"description":  {"Operations manager"},
		"capabilities": {"invoices.manage", "bills.manage"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, form.Encode())
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	var n int
	db.QueryRow("SELECT COUNT(*) FROM roles WHERE name = 'manager'").Scan(&n)
	if n != 1 {
		t.Errorf("expected role 'manager' to exist, got count %d", n)
	}
}

func TestCreateRole_ValidationErrors(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	cases := []struct {
		name string
		form url.Values
	}{
		{"missing name", url.Values{"name": {""}, "description": {""}}},
		{"bad name format", url.Values{"name": {"Bad Name!"}, "description": {""}}},
		{"reserved admin", url.Values{"name": {"admin"}, "description": {""}}},
		{"unknown capability", url.Values{"name": {"newrole"}, "capabilities": {"bogus.cap"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, tc.form.Encode())
			resp, err := noRedirect.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200 (re-render), got %d", resp.StatusCode)
			}
		})
	}
}

func TestCreateRole_DuplicateName(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{"name": {"bookkeeper"}, "description": {"dup"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, form.Encode())
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

func TestEditRole_AdminRoleBlocked(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/roles/admin/edit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("admin role edit should be forbidden, got %d", resp.StatusCode)
	}
}

func TestUpdateRole_BookkeeperCapabilities(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{
		"description":  {"Bookkeeper (narrowed)"},
		"capabilities": {"reports.view"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles/bookkeeper", cookies, form.Encode())
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	var caps string
	db.QueryRow("SELECT capabilities FROM roles WHERE name = 'bookkeeper'").Scan(&caps)
	if !strings.Contains(caps, "reports.view") || strings.Contains(caps, "invoices.manage") {
		t.Errorf("bookkeeper capabilities not updated correctly: %s", caps)
	}
}

func TestDeleteRole_SystemBlocked(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/roles/viewer", cookies, "")
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// System roles redirect with flash; role still exists.
	var n int
	db.QueryRow("SELECT COUNT(*) FROM roles WHERE name = 'viewer'").Scan(&n)
	if n != 1 {
		t.Errorf("viewer role should still exist")
	}
}

func TestDeleteRole_WithAssignedUsersBlocked(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Create a custom role, then a user with that role.
	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := url.Values{"name": {"custom"}, "description": {"c"}, "capabilities": {"reports.view"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, createForm.Encode())
	resp, _ := noRedirect.Do(req)
	resp.Body.Close()

	db.Exec("INSERT INTO users (username, password, full_name, role) VALUES ('assignedu','pw','A','custom')")

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/roles/custom", cookies, "")
	resp2, err := noRedirect.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	var n int
	db.QueryRow("SELECT COUNT(*) FROM roles WHERE name = 'custom'").Scan(&n)
	if n != 1 {
		t.Error("custom role should not be deleted while users are assigned")
	}
}

func TestDeleteRole_CustomUnassignedSucceeds(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	createForm := url.Values{"name": {"tempdel"}, "description": {""}, "capabilities": {"reports.view"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, createForm.Encode())
	resp, _ := noRedirect.Do(req)
	resp.Body.Close()

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/roles/tempdel", cookies, "")
	resp2, err := noRedirect.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	var n int
	db.QueryRow("SELECT COUNT(*) FROM roles WHERE name = 'tempdel'").Scan(&n)
	if n != 0 {
		t.Errorf("expected role deleted, got count %d", n)
	}
}

func TestNewRole_RendersForm(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/roles/new", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestEditRole_RendersFormForSystemRole(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/roles/bookkeeper/edit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for bookkeeper edit form, got %d", resp.StatusCode)
	}
}

func TestEditRole_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/roles/nosuchrole/edit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateRole_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{"description": {"x"}, "capabilities": {"reports.view"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles/ghost", cookies, form.Encode())
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateRole_AdminBlocked(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{"description": {"hacked"}, "capabilities": {"reports.view"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles/admin", cookies, form.Encode())
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestUpdateRole_InvalidCapabilityReRenders(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := url.Values{"description": {"x"}, "capabilities": {"not.a.real.cap"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles/bookkeeper", cookies, form.Encode())
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render on invalid cap), got %d", resp.StatusCode)
	}
}

func TestDeleteRole_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/roles/ghost", cookies, "")
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteRole_HTMXResponse(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	// Create a custom role to delete
	createForm := url.Values{"name": {"htmxdel"}, "description": {""}, "capabilities": {"reports.view"}}
	req0, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, createForm.Encode())
	resp0, _ := noRedirect.Do(req0)
	resp0.Body.Close()

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/roles/htmxdel", cookies, "")
	req.Header.Set("HX-Request", "true")
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("HTMX delete should return 200, got %d", resp.StatusCode)
	}
}

// --- Capability integration tests: real write routes gated by capability ---

func TestBookkeeper_CanCreateIncome(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsBookkeeper(t, ts, db)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=BK+income&amount=1000000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("bookkeeper should be able to create income, got %d", resp.StatusCode)
	}
}

func TestBookkeeper_CanCreateInvoice(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsBookkeeper(t, ts, db)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('BK Customer', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'BK Customer'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=Service&line_account_id=%d&line_quantity=100&line_unit_price=1000000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("bookkeeper should be able to create invoice, got %d", resp.StatusCode)
	}
}

func TestBookkeeper_CannotCreateAccount(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsBookkeeper(t, ts, db)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := "code=9-7777&name=BK+Account&account_type=asset&normal_balance=debit&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bookkeeper should NOT be able to create accounts, got %d", resp.StatusCode)
	}
}

func TestBookkeeper_CannotAccessUsers(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsBookkeeper(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/users", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bookkeeper should NOT access /users, got %d", resp.StatusCode)
	}
}

func TestBookkeeper_CannotAccessRoles(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsBookkeeper(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/roles", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("bookkeeper should NOT access /roles, got %d", resp.StatusCode)
	}
}

func TestViewer_CannotCreateInvoice(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := "contact_id=1&invoice_date=2026-04-04&due_date=2026-04-30"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer should get 403 on POST /invoices, got %d", resp.StatusCode)
	}
}

func TestCapabilityChange_TakesEffectOnNextRequest(t *testing.T) {
	ts, db := testServer(t)
	adminCookies := loginAsAdmin(t, ts)
	bkCookies := loginAsBookkeeper(t, ts, db)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Admin narrows bookkeeper to reports.view only.
	narrow := url.Values{"description": {"narrowed"}, "capabilities": {"reports.view"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles/bookkeeper", adminCookies, narrow.Encode())
	resp, _ := noRedirect.Do(req)
	resp.Body.Close()

	// Bookkeeper should now lose access to invoices.manage.
	form := "contact_id=1&invoice_date=2026-04-04&due_date=2026-04-30"
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", bkCookies, form)
	resp2, _ := noRedirect.Do(req2)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("narrowed bookkeeper should get 403 on /invoices, got %d", resp2.StatusCode)
	}
}
