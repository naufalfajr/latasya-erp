package handler_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestNewExpense_Form(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/expenses/new", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestEditExpense_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/expenses/999999/edit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestEditExpense_WrongSourceType creates an income journal entry (source
// "income") and confirms EditExpense 404s on it — the handler requires
// SourceType == expense, so this guards the "wrong source type" branch
// distinct from "no such id".
func TestEditExpense_WrongSourceType(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Not+an+expense&amount=1000000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("seed income entry: expected 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	entryID := strings.TrimPrefix(loc, "/journals/")

	client := &http.Client{}
	reqEdit, _ := requestWithCookies(db, "GET", ts.URL+"/expenses/"+entryID+"/edit", cookies, "")
	respEdit, err := client.Do(reqEdit)
	if err != nil {
		t.Fatal(err)
	}
	defer respEdit.Body.Close()
	if respEdit.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for income entry edited as expense, got %d", respEdit.StatusCode)
	}
}

func TestUpdateExpense_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses/999999", cookies, "entry_date=2026-04-04&description=x&amount=1000")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateExpense_ValidationError(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Diesel&amount=500000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, createForm)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create expense: expected 303, got %d", resp.StatusCode)
	}
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	client := &http.Client{}
	badForm := "entry_date=&description=&amount=0&expense_account=&payment_account="
	reqUpdate, _ := requestWithCookies(db, "POST", ts.URL+"/expenses/"+entryID, cookies, badForm)
	respUpdate, err := client.Do(reqUpdate)
	if err != nil {
		t.Fatal(err)
	}
	defer respUpdate.Body.Close()
	if respUpdate.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", respUpdate.StatusCode)
	}
}

func TestUpdateExpense_SuccessWithEditForm(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Diesel&amount=500000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, createForm)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	// Edit form should reflect the seeded amount/accounts.
	client := &http.Client{}
	reqEdit, _ := requestWithCookies(db, "GET", ts.URL+"/expenses/"+entryID+"/edit", cookies, "")
	respEdit, err := client.Do(reqEdit)
	if err != nil {
		t.Fatal(err)
	}
	defer respEdit.Body.Close()
	if respEdit.StatusCode != http.StatusOK {
		t.Errorf("edit expense: expected 200, got %d", respEdit.StatusCode)
	}

	updateForm := fmt.Sprintf(
		"entry_date=2026-04-05&description=Diesel+updated&amount=750000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	reqUpdate, _ := requestWithCookies(db, "POST", ts.URL+"/expenses/"+entryID, cookies, updateForm)
	respUpdate, err := noRedirect.Do(reqUpdate)
	if err != nil {
		t.Fatal(err)
	}
	defer respUpdate.Body.Close()
	if respUpdate.StatusCode != http.StatusSeeOther {
		t.Errorf("update expense: expected 303, got %d", respUpdate.StatusCode)
	}

	var desc string
	db.QueryRow("SELECT description FROM journal_entries WHERE id = ?", entryID).Scan(&desc)
	if desc != "Diesel updated" {
		t.Errorf("expected updated description, got %q", desc)
	}
	var totalDebit int
	db.QueryRow("SELECT COALESCE(SUM(debit),0) FROM journal_lines WHERE entry_id = ?", entryID).Scan(&totalDebit)
	if totalDebit != 750000 {
		t.Errorf("expected updated debit total 750000, got %d", totalDebit)
	}
}

func TestDeleteExpense_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/expenses/999999", cookies, "")
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// DeleteJournalEntryBySource errors for a missing id, which the handler
	// turns into a flash + redirect rather than a hard 404.
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect (delete rejected), got %d", resp.StatusCode)
	}
}

func TestDeleteExpense_WrongSourceType(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Not+an+expense&amount=1000000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	reqDelete, _ := requestWithCookies(db, "DELETE", ts.URL+"/expenses/"+entryID, cookies, "")
	respDelete, err := noRedirect.Do(reqDelete)
	if err != nil {
		t.Fatal(err)
	}
	defer respDelete.Body.Close()
	if respDelete.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect (delete rejected, wrong source), got %d", respDelete.StatusCode)
	}
	var stillExists int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", entryID).Scan(&stillExists)
	if stillExists != 1 {
		t.Errorf("income entry should not have been deleted via /expenses")
	}
}

func TestDeleteExpense_Success_HTMX(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Diesel+to+delete&amount=500000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, createForm)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	client := &http.Client{}
	reqDelete, _ := requestWithCookies(db, "DELETE", ts.URL+"/expenses/"+entryID, cookies, "")
	reqDelete.Header.Set("HX-Request", "true")
	respDelete, err := client.Do(reqDelete)
	if err != nil {
		t.Fatal(err)
	}
	defer respDelete.Body.Close()
	if respDelete.StatusCode != http.StatusOK {
		t.Errorf("htmx delete expense: expected 200, got %d", respDelete.StatusCode)
	}
	var remaining int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", entryID).Scan(&remaining)
	if remaining != 0 {
		t.Errorf("expense entry should have been deleted, found %d rows", remaining)
	}
}

func TestExpenses_ViewerDenied(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, "entry_date=2026-04-04&description=x&amount=1000")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer creating an expense should get 403, got %d", resp.StatusCode)
	}
}
