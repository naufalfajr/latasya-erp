package handler_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

// Happy-path Update/Delete tests for the financial-mutation handlers.
// These are deliberately end-to-end (HTTP → handler → DB) with post-mutation
// DB assertions — the interesting regression is "did the database change
// the way the user expected?", which is only visible at this level.

// --- helpers -----------------------------------------------------------------

func noRedirectClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// --- Invoice ----------------------------------------------------------------

func TestUpdateInvoice_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Inv Customer', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Inv Customer'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	// Create draft invoice.
	createForm := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=Original&line_account_id=%d&line_quantity=1&line_unit_price=1000000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, createForm)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var invID int
	db.QueryRow("SELECT id FROM invoices ORDER BY id DESC LIMIT 1").Scan(&invID)

	// Update: bump unit price and change description.
	updateForm := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=Updated&line_account_id=%d&line_quantity=1&line_unit_price=1500000",
		contactID, revenueID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/"+strconv.Itoa(invID), cookies, updateForm)
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp2.StatusCode)
	}

	var total int
	var desc string
	db.QueryRow("SELECT total FROM invoices WHERE id = ?", invID).Scan(&total)
	db.QueryRow("SELECT description FROM invoice_lines WHERE invoice_id = ?", invID).Scan(&desc)
	if total != 1500000 {
		t.Errorf("total = %d, want 1500000", total)
	}
	if desc != "Updated" {
		t.Errorf("line description = %q, want Updated", desc)
	}
}

func TestDeleteInvoice_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Del Customer', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Del Customer'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	form := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=x&line_account_id=%d&line_quantity=1&line_unit_price=100000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, form)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var invID int
	db.QueryRow("SELECT id FROM invoices ORDER BY id DESC LIMIT 1").Scan(&invID)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/invoices/"+strconv.Itoa(invID), cookies, "")
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	var n int
	db.QueryRow("SELECT COUNT(*) FROM invoices WHERE id = ?", invID).Scan(&n)
	if n != 0 {
		t.Errorf("invoice should be deleted, count = %d", n)
	}
}

// --- Bill -------------------------------------------------------------------

func TestUpdateBill_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Bill Supplier', 'supplier', 1)")
	var contactID, expenseID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Bill Supplier'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&expenseID)

	createForm := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-04-30&line_description=Fuel&line_account_id=%d&line_quantity=1&line_unit_price=500000",
		contactID, expenseID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, createForm)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var billID int
	db.QueryRow("SELECT id FROM bills ORDER BY id DESC LIMIT 1").Scan(&billID)

	updateForm := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-04-30&line_description=Fuel+updated&line_account_id=%d&line_quantity=1&line_unit_price=750000",
		contactID, expenseID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/bills/"+strconv.Itoa(billID), cookies, updateForm)
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp2.StatusCode)
	}

	var total int
	db.QueryRow("SELECT total FROM bills WHERE id = ?", billID).Scan(&total)
	if total != 750000 {
		t.Errorf("total = %d, want 750000", total)
	}
}

func TestDeleteBill_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Bill Supplier2', 'supplier', 1)")
	var contactID, expenseID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Bill Supplier2'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&expenseID)

	form := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-04-30&line_description=x&line_account_id=%d&line_quantity=1&line_unit_price=100000",
		contactID, expenseID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, form)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var billID int
	db.QueryRow("SELECT id FROM bills ORDER BY id DESC LIMIT 1").Scan(&billID)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/bills/"+strconv.Itoa(billID), cookies, "")
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	var n int
	db.QueryRow("SELECT COUNT(*) FROM bills WHERE id = ?", billID).Scan(&n)
	if n != 0 {
		t.Errorf("bill should be deleted, count = %d", n)
	}
}

// --- Income -----------------------------------------------------------------

func TestUpdateIncome_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Original+income&amount=1000000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, createForm)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var entryID int
	db.QueryRow("SELECT id FROM journal_entries WHERE source_type = 'income' ORDER BY id DESC LIMIT 1").Scan(&entryID)

	updateForm := fmt.Sprintf(
		"entry_date=2026-04-05&description=Updated+income&amount=1500000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/income/"+strconv.Itoa(entryID), cookies, updateForm)
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp2.StatusCode)
	}

	var description string
	var debit int
	db.QueryRow("SELECT description FROM journal_entries WHERE id = ?", entryID).Scan(&description)
	db.QueryRow("SELECT debit FROM journal_lines WHERE entry_id = ? AND debit > 0", entryID).Scan(&debit)
	if description != "Updated income" {
		t.Errorf("description = %q, want 'Updated income'", description)
	}
	if debit != 1500000 {
		t.Errorf("debit = %d, want 1500000", debit)
	}
}

func TestDeleteIncome_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=To+be+deleted&amount=1000000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, form)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var entryID int
	db.QueryRow("SELECT id FROM journal_entries WHERE source_type = 'income' ORDER BY id DESC LIMIT 1").Scan(&entryID)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/income/"+strconv.Itoa(entryID), cookies, "")
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	var n int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", entryID).Scan(&n)
	if n != 0 {
		t.Errorf("income entry should be deleted, count = %d", n)
	}
}

// --- Expense ----------------------------------------------------------------

func TestUpdateExpense_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Diesel&amount=500000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, createForm)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var entryID int
	db.QueryRow("SELECT id FROM journal_entries WHERE source_type = 'expense' ORDER BY id DESC LIMIT 1").Scan(&entryID)

	updateForm := fmt.Sprintf(
		"entry_date=2026-04-05&description=Diesel+corrected&amount=750000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/expenses/"+strconv.Itoa(entryID), cookies, updateForm)
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp2.StatusCode)
	}

	var description string
	var debit int
	db.QueryRow("SELECT description FROM journal_entries WHERE id = ?", entryID).Scan(&description)
	db.QueryRow("SELECT debit FROM journal_lines WHERE entry_id = ? AND debit > 0", entryID).Scan(&debit)
	if description != "Diesel corrected" {
		t.Errorf("description = %q, want 'Diesel corrected'", description)
	}
	if debit != 750000 {
		t.Errorf("debit = %d, want 750000", debit)
	}
}

func TestDeleteExpense_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=To+delete&amount=300000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, form)
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var entryID int
	db.QueryRow("SELECT id FROM journal_entries WHERE source_type = 'expense' ORDER BY id DESC LIMIT 1").Scan(&entryID)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/expenses/"+strconv.Itoa(entryID), cookies, "")
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	var n int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", entryID).Scan(&n)
	if n != 0 {
		t.Errorf("expense entry should be deleted, count = %d", n)
	}
}

// --- Account ----------------------------------------------------------------

func TestUpdateAccount_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Create a non-system account we can freely edit.
	createForm := url.Values{
		"code":           {"1-9901"},
		"name":           {"Test Wallet"},
		"account_type":   {"asset"},
		"normal_balance": {"debit"},
		"is_active":      {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts", cookies, createForm.Encode())
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var accountID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-9901'").Scan(&accountID)

	updateForm := url.Values{
		"code":           {"1-9901"},
		"name":           {"Test Wallet Renamed"},
		"account_type":   {"asset"},
		"normal_balance": {"debit"},
		"description":    {"renamed via test"},
		"is_active":      {"on"},
	}
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/accounts/"+strconv.Itoa(accountID), cookies, updateForm.Encode())
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp2.StatusCode)
	}

	var name, description string
	db.QueryRow("SELECT name, COALESCE(description, '') FROM accounts WHERE id = ?", accountID).
		Scan(&name, &description)
	if name != "Test Wallet Renamed" {
		t.Errorf("name = %q, want 'Test Wallet Renamed'", name)
	}
	if description != "renamed via test" {
		t.Errorf("description = %q, want 'renamed via test'", description)
	}
}

func TestUpdateAccount_ValidationError(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var accountID int
	db.QueryRow("SELECT id FROM accounts LIMIT 1").Scan(&accountID)

	form := url.Values{
		"code":           {""}, // required
		"name":           {""}, // required
		"account_type":   {"asset"},
		"normal_balance": {"debit"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts/"+strconv.Itoa(accountID), cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render), got %d", resp.StatusCode)
	}
}

// --- Contact ----------------------------------------------------------------

func TestUpdateContact_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	createForm := url.Values{
		"name":         {"Original Co"},
		"contact_type": {"customer"},
		"email":        {"orig@example.com"},
		"is_active":    {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/contacts", cookies, createForm.Encode())
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var contactID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Original Co'").Scan(&contactID)

	updateForm := url.Values{
		"name":         {"Renamed Co"},
		"contact_type": {"customer"},
		"email":        {"new@example.com"},
		"is_active":    {"on"},
	}
	req2, _ := requestWithCookies(db, "POST", ts.URL+"/contacts/"+strconv.Itoa(contactID), cookies, updateForm.Encode())
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp2.StatusCode)
	}

	var name, email string
	db.QueryRow("SELECT name, COALESCE(email, '') FROM contacts WHERE id = ?", contactID).
		Scan(&name, &email)
	if name != "Renamed Co" || email != "new@example.com" {
		t.Errorf("after update: name=%q email=%q", name, email)
	}
}

func TestDeleteContact_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	createForm := url.Values{
		"name":         {"Doomed Co"},
		"contact_type": {"customer"},
		"is_active":    {"on"},
	}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/contacts", cookies, createForm.Encode())
	resp, _ := noRedirectClient().Do(req)
	resp.Body.Close()

	var contactID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Doomed Co'").Scan(&contactID)

	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/contacts/"+strconv.Itoa(contactID), cookies, "")
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	var n int
	db.QueryRow("SELECT COUNT(*) FROM contacts WHERE id = ?", contactID).Scan(&n)
	if n != 0 {
		t.Errorf("contact should be deleted, count = %d", n)
	}
}
