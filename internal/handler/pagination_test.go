package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
)

// TestListJournals_Pagination seeds more than one page of entries and verifies
// the journals list paginates: page 1 shows 50 rows + "Page 1 of 2", page 2
// shows the remaining row.
func TestListJournals_Pagination(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	for i := 0; i < 51; i++ {
		if _, err := model.CreateJournalEntry(db,
			&model.JournalEntry{EntryDate: "2026-04-04", Description: "E", SourceType: "manual", IsPosted: true, CreatedBy: 1},
			[]model.JournalLine{{AccountID: cashID, Debit: 1000}, {AccountID: revenueID, Credit: 1000}}); err != nil {
			t.Fatalf("seed entry %d: %v", i, err)
		}
	}

	client := &http.Client{}

	// Page 1: 50 rows + "Page 1 of 2".
	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	resp.Body.Close()
	if !strings.Contains(body, "Page 1 of 2") {
		t.Errorf("page 1 should show 'Page 1 of 2'")
	}
	if n := strings.Count(body, `<tr id="journal-`); n != 50 {
		t.Errorf("page 1 should show 50 rows, got %d", n)
	}

	// Page 2: the 51st row + "Page 2 of 2".
	req2, _ := requestWithCookies(db, "GET", ts.URL+"/journals?page=2", cookies, "")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	body2 := readBody(t, resp2)
	resp2.Body.Close()
	if !strings.Contains(body2, "Page 2 of 2") {
		t.Errorf("page 2 should show 'Page 2 of 2'")
	}
	if n := strings.Count(body2, `<tr id="journal-`); n != 1 {
		t.Errorf("page 2 should show 1 row, got %d", n)
	}
}

// TestListIncome_PaginationPartial exercises the shared full-page pagination
// partial (PageNav.NextURL + the <a href> link), which the journals htmx test
// does not cover.
func TestListIncome_PaginationPartial(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	for i := 0; i < 51; i++ {
		if _, err := model.CreateJournalEntry(db,
			&model.JournalEntry{EntryDate: "2026-04-04", Description: "I", SourceType: model.SourceIncome, IsPosted: true, CreatedBy: 1},
			[]model.JournalLine{{AccountID: cashID, Debit: 1000}, {AccountID: revenueID, Credit: 1000}}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/income", cookies, "")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readBody(t, resp)
	if !strings.Contains(body, "Page 1 of 2") {
		t.Error("income page 1 should show 'Page 1 of 2'")
	}
	if !strings.Contains(body, "page=2") {
		t.Error("income page 1 should render a Next link to page=2")
	}
}

// TestListCreditNotes_Renders guards the credit-notes list template, which
// gained a pagination block (no dedicated render test existed before).
func TestListCreditNotes_Renders(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/credit-notes", cookies, "")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if body := readBody(t, resp); !strings.Contains(body, "Credit Notes") {
		t.Error("body should contain 'Credit Notes'")
	}
}
