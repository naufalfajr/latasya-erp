package handler_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// auditRow captures the key columns we assert against in event tests.
type auditRow struct {
	Action      string
	Actor       string
	TargetType  string
	TargetID    sql.NullInt64
	TargetLabel string
	Result      string
	Metadata    string
}

// latestAuditFor fetches the most recent audit_log row for a given action.
// Fails the test if no such row exists.
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

// --- Auth: login / logout / password ----------------------------------------

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
	client := noRedirectClient()
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
	client := noRedirectClient()
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

	req, _ := requestWithCookies(db, "POST", ts.URL+"/logout", cookies, "")
	resp, err := noRedirectClient().Do(req)
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

	form := url.Values{
		"current_password": {adminTestPassword},
		"new_password":     {"NewPass12345"},
		"confirm_password": {"NewPass12345"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/password/change", cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
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

// --- User CRUD --------------------------------------------------------------

func TestAudit_UserCreate(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"username":  {"newuser1"},
		"full_name": {"New User"},
		"password":  {"password123"},
		"role":      {"viewer"},
		"is_active": {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
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

	createForm := url.Values{
		"username":  {"editme"},
		"full_name": {"Original"},
		"password":  {"password123"},
		"role":      {"viewer"},
		"is_active": {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, createForm.Encode())
	resp, _ := noRedirectClient().Do(req)
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
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	r := latestAuditFor(t, db, "user.update")
	if r.TargetLabel != "editme" {
		t.Errorf("target_label = %q, want editme", r.TargetLabel)
	}
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

	createForm := url.Values{
		"username":  {"deleteme"},
		"full_name": {"Delete Me"},
		"password":  {"password123"},
		"role":      {"viewer"},
		"is_active": {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, createForm.Encode())
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var id int
	db.QueryRow("SELECT id FROM users WHERE username = 'deleteme'").Scan(&id)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/users/"+strconv.Itoa(id), cookies, "")
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	r := latestAuditFor(t, db, "user.delete")
	if r.TargetLabel != "deleteme" {
		t.Errorf("target_label = %q, want deleteme", r.TargetLabel)
	}
	if !strings.Contains(r.Metadata, "is_active") {
		t.Errorf("metadata should describe is_active flip, got %q", r.Metadata)
	}
}

// --- Role CRUD --------------------------------------------------------------

func TestAudit_RoleCreate(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"name":         {"newrole"},
		"description":  {"New role"},
		"capabilities": {"reports.view"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, form.Encode())
	resp, _ := noRedirectClient().Do(req)
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

	form := url.Values{
		"description":  {"narrowed"},
		"capabilities": {"reports.view"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles/bookkeeper", cookies, form.Encode())
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	r := latestAuditFor(t, db, "role.update")
	if r.TargetLabel != "bookkeeper" {
		t.Errorf("target_label = %q, want bookkeeper", r.TargetLabel)
	}
	if !strings.Contains(r.Metadata, "capabilities") {
		t.Errorf("metadata should include capabilities diff, got %q", r.Metadata)
	}
}

func TestAudit_RoleDelete(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	createForm := url.Values{"name": {"doomed"}, "description": {"x"}, "capabilities": {"reports.view"}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/roles", cookies, createForm.Encode())
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/roles/doomed", cookies, "")
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	r := latestAuditFor(t, db, "role.delete")
	if r.TargetLabel != "doomed" {
		t.Errorf("target_label = %q, want doomed", r.TargetLabel)
	}
}

// --- Invoice ----------------------------------------------------------------

func TestAudit_InvoiceCreateAndDelete(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Audit Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Audit Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	form := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=x&line_account_id=%d&line_quantity=1&line_unit_price=750000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, form)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var invID int
	db.QueryRow("SELECT id FROM invoices ORDER BY id DESC LIMIT 1").Scan(&invID)

	createRow := latestAuditFor(t, db, "invoice.create")
	if createRow.Actor != "admin" {
		t.Errorf("invoice.create actor = %q, want admin", createRow.Actor)
	}
	if !strings.Contains(createRow.Metadata, "750000") {
		t.Errorf("invoice.create should contain total 750000, got %q", createRow.Metadata)
	}

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/invoices/"+strconv.Itoa(invID), cookies, "")
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	delRow := latestAuditFor(t, db, "invoice.delete")
	if !strings.Contains(delRow.Metadata, `"before"`) {
		t.Errorf("invoice.delete should include before snapshot, got %q", delRow.Metadata)
	}
}

func TestAudit_InvoiceUpdate_DiffsChangedFields(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Upd Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Upd Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	createForm := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=x&line_account_id=%d&line_quantity=1&line_unit_price=1000000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, createForm)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var invID int
	db.QueryRow("SELECT id FROM invoices ORDER BY id DESC LIMIT 1").Scan(&invID)

	// Change unit_price → total changes. Other fields unchanged.
	updateForm := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=x&line_account_id=%d&line_quantity=1&line_unit_price=1500000",
		contactID, revenueID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/"+strconv.Itoa(invID), cookies, updateForm)
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	row := latestAuditFor(t, db, "invoice.update")
	if !strings.Contains(row.Metadata, "1000000") || !strings.Contains(row.Metadata, "1500000") {
		t.Errorf("invoice.update metadata should show both old and new total, got %q", row.Metadata)
	}
	if strings.Contains(row.Metadata, "due_date") {
		t.Errorf("invoice.update should not include unchanged due_date, got %q", row.Metadata)
	}
}

func TestAudit_InvoiceSend(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Send Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Send Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	createForm := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=x&line_account_id=%d&line_quantity=1&line_unit_price=500000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, createForm)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var invID int
	db.QueryRow("SELECT id FROM invoices ORDER BY id DESC LIMIT 1").Scan(&invID)

	req2, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/send", cookies, "")
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	row := latestAuditFor(t, db, "invoice.send")
	if !strings.Contains(row.Metadata, "sent") {
		t.Errorf("invoice.send metadata should reflect status transition, got %q", row.Metadata)
	}
}

// --- Bill -------------------------------------------------------------------

func TestAudit_BillCreateUpdateDelete(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Bill Audit', 'supplier', 1)")
	var contactID, expenseID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Bill Audit'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&expenseID)

	createForm := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-04-30&line_description=Fuel&line_account_id=%d&line_quantity=1&line_unit_price=400000",
		contactID, expenseID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, createForm)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	createRow := latestAuditFor(t, db, "bill.create")
	if !strings.Contains(createRow.Metadata, "400000") {
		t.Errorf("bill.create should contain total, got %q", createRow.Metadata)
	}

	var billID int
	db.QueryRow("SELECT id FROM bills ORDER BY id DESC LIMIT 1").Scan(&billID)

	updateForm := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-04-30&line_description=Fuel&line_account_id=%d&line_quantity=1&line_unit_price=600000",
		contactID, expenseID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/bills/"+strconv.Itoa(billID), cookies, updateForm)
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	updateRow := latestAuditFor(t, db, "bill.update")
	if !strings.Contains(updateRow.Metadata, "600000") {
		t.Errorf("bill.update should reflect new total 600000, got %q", updateRow.Metadata)
	}

	req3, _ := requestWithCookies(db, "DELETE", ts.URL+"/bills/"+strconv.Itoa(billID), cookies, "")
	resp3, _ := noRedirectClient().Do(req3)
	resp3.Body.Close()

	delRow := latestAuditFor(t, db, "bill.delete")
	if !strings.Contains(delRow.Metadata, `"before"`) {
		t.Errorf("bill.delete should include before snapshot, got %q", delRow.Metadata)
	}
}

// --- Journal ----------------------------------------------------------------

func TestAudit_JournalCreateAndDelete(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	form := url.Values{}
	form.Set("entry_date", "2026-04-04")
	form.Set("description", "Audit test entry")
	form.Add("line_account_id", strconv.Itoa(cashID))
	form.Add("line_debit", "1000000")
	form.Add("line_credit", "")
	form.Add("line_memo", "cash in")
	form.Add("line_account_id", strconv.Itoa(revenueID))
	form.Add("line_debit", "")
	form.Add("line_credit", "1000000")
	form.Add("line_memo", "revenue")

	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals", cookies, form.Encode())
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	row := latestAuditFor(t, db, "journal.create")
	if row.TargetLabel != "Audit test entry" {
		t.Errorf("journal.create target_label = %q, want 'Audit test entry'", row.TargetLabel)
	}
	if !strings.Contains(row.Metadata, "1000000") {
		t.Errorf("journal.create metadata should contain total 1000000, got %q", row.Metadata)
	}

	var entryID int
	db.QueryRow("SELECT id FROM journal_entries WHERE source_type = 'manual' ORDER BY id DESC LIMIT 1").Scan(&entryID)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/journals/"+strconv.Itoa(entryID), cookies, "")
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	delRow := latestAuditFor(t, db, "journal.delete")
	if delRow.TargetLabel != "Audit test entry" {
		t.Errorf("journal.delete target_label = %q", delRow.TargetLabel)
	}
}

// --- Income / Expense -------------------------------------------------------

func TestAudit_IncomeCreateAndDelete(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Audit+income&amount=2000000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, form)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	createRow := latestAuditFor(t, db, "income.create")
	if !strings.Contains(createRow.Metadata, "2000000") {
		t.Errorf("income.create should contain amount, got %q", createRow.Metadata)
	}

	var entryID int
	db.QueryRow("SELECT id FROM journal_entries WHERE source_type = 'income' ORDER BY id DESC LIMIT 1").Scan(&entryID)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/income/"+strconv.Itoa(entryID), cookies, "")
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	delRow := latestAuditFor(t, db, "income.delete")
	if !strings.Contains(delRow.Metadata, `"before"`) {
		t.Errorf("income.delete should include before snapshot, got %q", delRow.Metadata)
	}
}

func TestAudit_ExpenseUpdate_DiffsAmount(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Diesel&amount=300000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, createForm)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var entryID int
	db.QueryRow("SELECT id FROM journal_entries WHERE source_type = 'expense' ORDER BY id DESC LIMIT 1").Scan(&entryID)

	updateForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Diesel&amount=500000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/expenses/"+strconv.Itoa(entryID), cookies, updateForm)
	resp2, _ := noRedirectClient().Do(req2)
	resp2.Body.Close()

	row := latestAuditFor(t, db, "expense.update")
	if !strings.Contains(row.Metadata, "300000") || !strings.Contains(row.Metadata, "500000") {
		t.Errorf("expense.update should contain before/after amounts, got %q", row.Metadata)
	}
	if strings.Contains(row.Metadata, "entry_date") {
		t.Errorf("expense.update should not include unchanged entry_date, got %q", row.Metadata)
	}
}
