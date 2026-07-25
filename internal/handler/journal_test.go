package handler_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
)

// All routes exercised here (GET/POST /journals/{id}/edit, DELETE
// /journals/{id}, GET /journals/{id}) are already wired into the shared
// testServer in handler_test.go, so no custom mux is needed for this file.

// mustCreateManualJournal creates a balanced, posted manual journal entry
// directly via the model layer and returns its ID.
func mustCreateManualJournal(t *testing.T, db *sql.DB, description string) int {
	t.Helper()
	var adminID, cashID, revenueID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate:   "2026-04-04",
		Description: description,
		SourceType:  model.SourceManual,
		IsPosted:    true,
		CreatedBy:   adminID,
	}
	lines := []model.JournalLine{
		{AccountID: cashID, Debit: 1000000},
		{AccountID: revenueID, Credit: 1000000},
	}
	id, err := model.CreateJournalEntry(db, je, lines)
	if err != nil {
		t.Fatalf("create manual journal: %v", err)
	}
	return id
}

// mustCreateIncomeJournal creates a non-manual (auto-generated) journal
// entry, the kind EditJournal/DeleteJournal refuse to mutate directly.
func mustCreateIncomeJournal(t *testing.T, db *sql.DB, description string) int {
	t.Helper()
	var adminID, cashID, revenueID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate:   "2026-04-04",
		Description: description,
		SourceType:  model.SourceIncome,
		IsPosted:    true,
		CreatedBy:   adminID,
	}
	lines := []model.JournalLine{
		{AccountID: cashID, Debit: 500000},
		{AccountID: revenueID, Credit: 500000},
	}
	id, err := model.CreateJournalEntry(db, je, lines)
	if err != nil {
		t.Fatalf("create income journal: %v", err)
	}
	return id
}

// --- ViewJournal ---------------------------------------------------------------

func TestViewJournal_InvalidID(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals/notanumber", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestViewJournal_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals/9999999", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// --- EditJournal -----------------------------------------------------------

func TestEditJournal_Success(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustCreateManualJournal(t, db, "Edit form test")

	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals/"+strconv.Itoa(id)+"/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Edit form test") {
		t.Error("edit form should render the existing description")
	}
}

func TestEditJournal_InvalidID(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals/notanumber/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestEditJournal_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals/9999999/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestEditJournal_NonManualForbidden(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustCreateIncomeJournal(t, db, "Auto-generated income entry")

	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals/"+strconv.Itoa(id)+"/edit", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}
	if flash := flashValue(resp); flash != "Cannot edit auto-generated journal entries" {
		t.Errorf("flash = %q, want 'Cannot edit auto-generated journal entries'", flash)
	}
}

// --- UpdateJournal ---------------------------------------------------------

func TestUpdateJournal_Success(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustCreateManualJournal(t, db, "Original description")

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	form := fmt.Sprintf(
		"entry_date=2026-04-06&description=Updated+description&line_account_id=%d&line_account_id=%d&line_debit=2000000&line_debit=0&line_credit=0&line_credit=2000000&line_memo=&line_memo=",
		cashID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals/"+strconv.Itoa(id), cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/journals/"+strconv.Itoa(id) {
		t.Errorf("Location = %q, want /journals/%d", loc, id)
	}

	var desc string
	var totalDebit int
	db.QueryRow("SELECT description FROM journal_entries WHERE id = ?", id).Scan(&desc)
	if desc != "Updated description" {
		t.Errorf("description = %q, want 'Updated description'", desc)
	}
	db.QueryRow("SELECT COALESCE(SUM(debit),0) FROM journal_lines WHERE entry_id = ?", id).Scan(&totalDebit)
	if totalDebit != 2000000 {
		t.Errorf("total debit = %d, want 2000000", totalDebit)
	}
}

func TestUpdateJournal_InvalidID(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals/notanumber", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateJournal_NotFound(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals/9999999", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateJournal_ValidationError(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustCreateManualJournal(t, db, "Needs valid update")

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	// Unbalanced lines (5000 debit vs 3000 credit).
	form := fmt.Sprintf(
		"entry_date=2026-04-06&description=Unbalanced&line_account_id=%d&line_account_id=%d&line_debit=5000&line_debit=0&line_credit=0&line_credit=3000&line_memo=&line_memo=",
		cashID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals/"+strconv.Itoa(id), cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error re-render), got %d", resp.StatusCode)
	}
}

func TestUpdateJournal_ModelErrorBadAccount(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustCreateManualJournal(t, db, "Needs model error")

	// Balanced and non-zero, so validateJournal passes, but the account
	// doesn't exist so the model-level insert fails its foreign key.
	form := "entry_date=2026-04-06&description=Bad+account&line_account_id=9999999&line_account_id=9999998&line_debit=1000&line_debit=0&line_credit=0&line_credit=1000&line_memo=&line_memo="
	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals/"+strconv.Itoa(id), cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (model error re-render), got %d", resp.StatusCode)
	}
}

// --- DeleteJournal ---------------------------------------------------------

func TestDeleteJournal_InvalidID(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/journals/notanumber", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteJournal_NonManualErrors(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustCreateIncomeJournal(t, db, "Cannot delete me")

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/journals/"+strconv.Itoa(id), cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/journals/"+strconv.Itoa(id) {
		t.Errorf("Location = %q, want /journals/%d (error redirect, not list)", loc, id)
	}
	flash := flashValue(resp)
	if !strings.Contains(flash, "Error:") {
		t.Errorf("flash = %q, want an error message", flash)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", id).Scan(&count)
	if count != 1 {
		t.Error("non-manual journal entry should not have been deleted")
	}
}

func TestDeleteJournal_HTMX(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	id := mustCreateManualJournal(t, db, "HTMX delete test")

	req, _ := requestWithCookies(db, "DELETE", ts.URL+"/journals/"+strconv.Itoa(id), cookies, "")
	req.Header.Set("HX-Request", "true")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for HTMX delete, got %d", resp.StatusCode)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", id).Scan(&count)
	if count != 0 {
		t.Error("manual journal entry should have been deleted")
	}
}
