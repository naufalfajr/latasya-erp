package handler_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestNewIncome_Form(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/income/new", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestEditIncome_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/income/999999/edit", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestEditIncome_WrongSourceType creates an expense journal entry and
// confirms EditIncome 404s on it — the handler requires SourceType ==
// income, distinct from a plain "no such id" 404.
func TestEditIncome_WrongSourceType(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Not+income&amount=1000000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("seed expense entry: expected 303, got %d", resp.StatusCode)
	}
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	client := &http.Client{}
	reqEdit, _ := requestWithCookies(db, "GET", ts.URL+"/income/"+entryID+"/edit", cookies, "")
	respEdit, err := client.Do(reqEdit)
	if err != nil {
		t.Fatal(err)
	}
	defer respEdit.Body.Close()
	if respEdit.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for expense entry edited as income, got %d", respEdit.StatusCode)
	}
}

func TestUpdateIncome_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income/999999", cookies, "entry_date=2026-04-04&description=x&amount=1000")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateIncome_ValidationError(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Bus+fare&amount=500000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, createForm)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create income: expected 303, got %d", resp.StatusCode)
	}
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	client := &http.Client{}
	badForm := "entry_date=&description=&amount=0&revenue_account=&deposit_account="
	reqUpdate, _ := requestWithCookies(db, "POST", ts.URL+"/income/"+entryID, cookies, badForm)
	respUpdate, err := client.Do(reqUpdate)
	if err != nil {
		t.Fatal(err)
	}
	defer respUpdate.Body.Close()
	if respUpdate.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", respUpdate.StatusCode)
	}
}

func TestUpdateIncome_SuccessWithEditForm(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Bus+fare&amount=500000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, createForm)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	// Edit form should render the seeded entry.
	client := &http.Client{}
	reqEdit, _ := requestWithCookies(db, "GET", ts.URL+"/income/"+entryID+"/edit", cookies, "")
	respEdit, err := client.Do(reqEdit)
	if err != nil {
		t.Fatal(err)
	}
	defer respEdit.Body.Close()
	if respEdit.StatusCode != http.StatusOK {
		t.Errorf("edit income: expected 200, got %d", respEdit.StatusCode)
	}

	updateForm := fmt.Sprintf(
		"entry_date=2026-04-05&description=Bus+fare+updated&amount=750000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	reqUpdate, _ := requestWithCookies(db, "POST", ts.URL+"/income/"+entryID, cookies, updateForm)
	respUpdate, err := noRedirect.Do(reqUpdate)
	if err != nil {
		t.Fatal(err)
	}
	defer respUpdate.Body.Close()
	if respUpdate.StatusCode != http.StatusSeeOther {
		t.Errorf("update income: expected 303, got %d", respUpdate.StatusCode)
	}

	var desc string
	db.QueryRow("SELECT description FROM journal_entries WHERE id = ?", entryID).Scan(&desc)
	if desc != "Bus fare updated" {
		t.Errorf("expected updated description, got %q", desc)
	}
	var totalCredit int
	db.QueryRow("SELECT COALESCE(SUM(credit),0) FROM journal_lines WHERE entry_id = ?", entryID).Scan(&totalCredit)
	if totalCredit != 750000 {
		t.Errorf("expected updated credit total 750000, got %d", totalCredit)
	}
}

func TestDeleteIncome_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/income/999999", cookies, "")
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// The journal module errors for a missing id, which the handler
	// turns into a flash + redirect rather than a hard 404.
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect (delete rejected), got %d", resp.StatusCode)
	}
}

func TestDeleteIncome_WrongSourceType(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Not+income&amount=1000000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	reqDelete, _ := requestWithCookies(db, "DELETE", ts.URL+"/income/"+entryID, cookies, "")
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
		t.Errorf("expense entry should not have been deleted via /income")
	}
}

func TestDeleteIncome_Success_HTMX(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	createForm := fmt.Sprintf(
		"entry_date=2026-04-04&description=Bus+fare+to+delete&amount=500000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, createForm)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	entryID := strings.TrimPrefix(resp.Header.Get("Location"), "/journals/")

	client := &http.Client{}
	reqDelete, _ := requestWithCookies(db, "DELETE", ts.URL+"/income/"+entryID, cookies, "")
	reqDelete.Header.Set("HX-Request", "true")
	respDelete, err := client.Do(reqDelete)
	if err != nil {
		t.Fatal(err)
	}
	defer respDelete.Body.Close()
	if respDelete.StatusCode != http.StatusOK {
		t.Errorf("htmx delete income: expected 200, got %d", respDelete.StatusCode)
	}
	var remaining int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", entryID).Scan(&remaining)
	if remaining != 0 {
		t.Errorf("income entry should have been deleted, found %d rows", remaining)
	}
}

func TestIncome_ViewerDenied(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, "entry_date=2026-04-04&description=x&amount=1000")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer creating income should get 403, got %d", resp.StatusCode)
	}
}
