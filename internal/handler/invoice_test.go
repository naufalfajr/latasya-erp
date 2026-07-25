package handler_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// testServerForInvoiceBulkActions wires up only the bulk-action /
// recurring-invoice routes that aren't part of the shared testServer's route
// table (see handler_test.go), mirroring the pattern in
// testServerWithSchoolCalendar (school_calendar_test.go). Kept separate so we
// never touch handler_test.go, which other agents are editing concurrently.
func testServerForInvoiceBulkActions(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)

	protected := http.NewServeMux()
	protected.HandleFunc("POST /invoices/generate-recurring", auth.CapabilityOnly(model.CapInvoicesManage, h.GenerateRecurringInvoices))
	protected.HandleFunc("POST /invoices/bulk-delete", auth.CapabilityOnly(model.CapInvoicesManage, h.BulkDeleteInvoices))
	protected.HandleFunc("POST /invoices/bulk-send", auth.CapabilityOnly(model.CapInvoicesManage, h.BulkSendInvoices))
	protected.HandleFunc("GET /password/change", h.PasswordChangePage)
	protected.HandleFunc("POST /password/change", h.PasswordChange)

	mux.Handle("/", auth.RequireAuth(db, auth.CSRFProtect(h.EnforcePasswordChange(protected))))

	hash, err := auth.HashPassword(adminTestPassword)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	if _, err := db.Exec("UPDATE users SET password=?, must_change_password=0 WHERE username='admin'", hash); err != nil {
		t.Fatalf("update admin: %v", err)
	}

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, db
}

// seedCustomer inserts an active customer contact and returns its ID.
func seedCustomer(t *testing.T, db *sql.DB, name string) int {
	t.Helper()
	if _, err := db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES (?, 'customer', 1)", name); err != nil {
		t.Fatalf("seed customer %q: %v", name, err)
	}
	var id int
	if err := db.QueryRow("SELECT id FROM contacts WHERE name = ?", name).Scan(&id); err != nil {
		t.Fatalf("get customer id: %v", err)
	}
	return id
}

// mustCreateDraftInvoice creates a draft invoice directly via the model layer
// (bypassing HTTP/handler validation) so bulk-action tests can set up fixture
// invoices without going through the full form-submission flow.
func mustCreateDraftInvoice(t *testing.T, db *sql.DB, contactID, accountID, unitPrice int) int {
	t.Helper()
	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("get admin id: %v", err)
	}
	inv := &model.Invoice{
		ContactID:   contactID,
		InvoiceDate: "2026-04-04",
		DueDate:     "2026-04-30",
		CreatedBy:   adminID,
	}
	lines := []model.InvoiceLine{{Description: "Bus fee", Quantity: 100, UnitPrice: unitPrice, AccountID: accountID}}
	id, err := model.CreateInvoice(db, inv, lines)
	if err != nil {
		t.Fatalf("create draft invoice: %v", err)
	}
	return id
}

func accountID(t *testing.T, db *sql.DB, code string) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = ?", code).Scan(&id); err != nil {
		t.Fatalf("get account %q: %v", code, err)
	}
	return id
}

// --- GenerateRecurringInvoices ------------------------------------------------

func TestGenerateRecurringInvoices_CreatesDraftsForActiveCustomers(t *testing.T) {
	ts, db := testServerForInvoiceBulkActions(t)
	cookies := loginAsAdmin(t, ts)
	seedCustomer(t, db, "Recurring Customer")

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/generate-recurring", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/invoices" {
		t.Errorf("Location = %q, want /invoices", loc)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM invoices WHERE status = 'draft'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 draft invoice generated, got %d", count)
	}

	if flash := flashValue(resp); !strings.Contains(flash, "Generated 1 draft invoice(s)") {
		t.Errorf("flash = %q, want mention of 1 generated invoice", flash)
	}
}

func TestGenerateRecurringInvoices_NoDefaultRevenueAccount(t *testing.T) {
	ts, db := testServerForInvoiceBulkActions(t)
	cookies := loginAsAdmin(t, ts)
	seedCustomer(t, db, "No Default Account Customer")

	if _, err := db.Exec("UPDATE company_profile SET default_revenue_account_id = NULL WHERE id = 1"); err != nil {
		t.Fatalf("clear default revenue account: %v", err)
	}

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/generate-recurring", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashValue(resp); !strings.Contains(flash, "Error generating invoices") {
		t.Errorf("flash = %q, want an error message", flash)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM invoices").Scan(&count)
	if count != 0 {
		t.Errorf("expected no invoices created, got %d", count)
	}
}

func TestGenerateRecurringInvoices_PerCustomerFailureReported(t *testing.T) {
	ts, db := testServerForInvoiceBulkActions(t)
	cookies := loginAsAdmin(t, ts)
	seedCustomer(t, db, "Dangling Account Customer")

	// Point the default revenue account at a nonexistent account: nonzero so
	// the top-level "no default account" guard passes, but every per-customer
	// invoice insert then fails its account_id foreign key. Foreign keys are
	// toggled off just for this write since the column itself has an FK to
	// accounts(id) — the single test DB connection (SetMaxOpenConns(1)) makes
	// this safe to flip back on immediately after.
	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("disable foreign_keys: %v", err)
	}
	if _, err := db.Exec("UPDATE company_profile SET default_revenue_account_id = 999999 WHERE id = 1"); err != nil {
		t.Fatalf("dangle default revenue account: %v", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("re-enable foreign_keys: %v", err)
	}

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/generate-recurring", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashValue(resp); !strings.Contains(flash, "Failed 1") {
		t.Errorf("flash = %q, want mention of 1 failure", flash)
	}
}

// --- BulkDeleteInvoices --------------------------------------------------------

func TestBulkDeleteInvoices_NoneSelected(t *testing.T) {
	ts, db := testServerForInvoiceBulkActions(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/bulk-delete", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashValue(resp); flash != "No invoices selected" {
		t.Errorf("flash = %q, want 'No invoices selected'", flash)
	}
}

func TestBulkDeleteInvoices_DeletesDraftsSkipsRest(t *testing.T) {
	ts, db := testServerForInvoiceBulkActions(t)
	cookies := loginAsAdmin(t, ts)

	contactID := seedCustomer(t, db, "Bulk Delete Customer")
	revenueID := accountID(t, db, "4-1001")

	draft1 := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)
	draft2 := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)
	sentInv := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)
	var adminID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID)
	if err := model.SendInvoice(db, sentInv, adminID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	form := url.Values{}
	form.Add("ids", strconv.Itoa(draft1))
	form.Add("ids", strconv.Itoa(draft2))
	form.Add("ids", strconv.Itoa(sentInv))
	form.Add("ids", "9999999") // nonexistent

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/bulk-delete", cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	flash := flashValue(resp)
	if !strings.Contains(flash, "Deleted 2 draft invoice(s)") || !strings.Contains(flash, "Skipped 2") {
		t.Errorf("flash = %q, want mention of 2 deleted and 2 skipped", flash)
	}

	var remaining int
	db.QueryRow("SELECT COUNT(*) FROM invoices WHERE id IN (?, ?)", draft1, draft2).Scan(&remaining)
	if remaining != 0 {
		t.Errorf("expected drafts to be deleted, %d remain", remaining)
	}
	var sentStillThere int
	db.QueryRow("SELECT COUNT(*) FROM invoices WHERE id = ?", sentInv).Scan(&sentStillThere)
	if sentStillThere != 1 {
		t.Error("sent invoice should not have been deleted")
	}
}

// --- BulkSendInvoices ----------------------------------------------------------

func TestBulkSendInvoices_NoneSelected(t *testing.T) {
	ts, db := testServerForInvoiceBulkActions(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/bulk-send", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashValue(resp); flash != "No invoices selected" {
		t.Errorf("flash = %q, want 'No invoices selected'", flash)
	}
}

func TestBulkSendInvoices_SendsSkipsAndReportsFailures(t *testing.T) {
	ts, db := testServerForInvoiceBulkActions(t)
	cookies := loginAsAdmin(t, ts)

	contactID := seedCustomer(t, db, "Bulk Send Customer")
	revenueID := accountID(t, db, "4-1001")

	okDraft := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)
	// A zero-price line makes the invoice total 0, which makes SendInvoice's
	// underlying journal entry fail balance validation ("must have at least
	// one debit and credit line") — a realistic per-invoice send failure.
	zeroTotalDraft := mustCreateDraftInvoice(t, db, contactID, revenueID, 0)

	var adminID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID)
	alreadySent := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)
	if err := model.SendInvoice(db, alreadySent, adminID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	form := url.Values{}
	form.Add("ids", strconv.Itoa(okDraft))
	form.Add("ids", strconv.Itoa(zeroTotalDraft))
	form.Add("ids", strconv.Itoa(alreadySent))
	form.Add("ids", "9999999") // nonexistent -> skipped

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/bulk-send", cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	flash := flashValue(resp)
	if !strings.Contains(flash, "Marked 1 invoice(s) as sent") {
		t.Errorf("flash = %q, want 1 sent", flash)
	}
	if !strings.Contains(flash, "Skipped 2") {
		t.Errorf("flash = %q, want 2 skipped (already sent + nonexistent)", flash)
	}
	if !strings.Contains(flash, "Failed 1") {
		t.Errorf("flash = %q, want 1 failed", flash)
	}

	var status string
	db.QueryRow("SELECT status FROM invoices WHERE id = ?", okDraft).Scan(&status)
	if status != "sent" {
		t.Errorf("okDraft status = %q, want sent", status)
	}
}

// --- CreateInvoice --------------------------------------------------------------

func TestCreateInvoice_ValidationErrors(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	revenueID := accountID(t, db, "4-1001")

	form := url.Values{}
	form.Set("contact_id", "")
	form.Set("invoice_date", "")
	form.Set("due_date", "")
	// Three lines, each missing exactly one required field, so every
	// per-line validation branch fires without the line being dropped by
	// parseInvoiceLines' "entirely empty" skip.
	form.Add("line_description", "")
	form.Add("line_quantity", "1")
	form.Add("line_unit_price", "100000")
	form.Add("line_account_id", strconv.Itoa(revenueID))

	form.Add("line_description", "Fee")
	form.Add("line_quantity", "1")
	form.Add("line_unit_price", "0")
	form.Add("line_account_id", strconv.Itoa(revenueID))

	form.Add("line_description", "Fee2")
	form.Add("line_quantity", "1")
	form.Add("line_unit_price", "100000")
	form.Add("line_account_id", "")

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, form.Encode())
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error re-render), got %d", resp.StatusCode)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM invoices").Scan(&count)
	if count != 0 {
		t.Errorf("expected no invoice created, got %d", count)
	}
}

func TestCreateInvoice_ValidationError_NoLines(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := seedCustomer(t, db, "No Lines Customer")

	form := fmt.Sprintf("contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30", contactID)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error re-render), got %d", resp.StatusCode)
	}
}

func TestCreateInvoice_ModelError_BadAccount(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := seedCustomer(t, db, "Bad Account Customer")

	form := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=Bus+fee&line_account_id=9999999&line_quantity=1&line_unit_price=100000",
		contactID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (model error re-render), got %d", resp.StatusCode)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM invoices").Scan(&count)
	if count != 0 {
		t.Errorf("expected no invoice created on FK failure, got %d", count)
	}
}

// --- EditInvoice ------------------------------------------------------------

func TestEditInvoice_InvalidID(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/notanumber/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestEditInvoice_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/9999999/edit", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestEditInvoice_NonDraftRedirects(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := seedCustomer(t, db, "Sent Invoice Customer")
	revenueID := accountID(t, db, "4-1001")
	invID := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)

	var adminID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID)
	if err := model.SendInvoice(db, invID, adminID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/edit", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect for non-draft invoice, got %d", resp.StatusCode)
	}
	if flash := flashValue(resp); flash != "Can only edit draft invoices" {
		t.Errorf("flash = %q, want 'Can only edit draft invoices'", flash)
	}
}

// --- UpdateInvoice ----------------------------------------------------------

func TestUpdateInvoice_InvalidID(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/notanumber", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateInvoice_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/9999999", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateInvoice_ValidationError(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := seedCustomer(t, db, "Update Validation Customer")
	revenueID := accountID(t, db, "4-1001")
	invID := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)

	form := fmt.Sprintf("contact_id=%d&invoice_date=&due_date=2026-04-30&line_description=Fee&line_account_id=%d&line_quantity=1&line_unit_price=100000", contactID, revenueID)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/"+strconv.Itoa(invID), cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error re-render), got %d", resp.StatusCode)
	}
}

func TestUpdateInvoice_ModelErrorOnNonDraft(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := seedCustomer(t, db, "Update Model Error Customer")
	revenueID := accountID(t, db, "4-1001")
	invID := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)

	var adminID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID)
	if err := model.SendInvoice(db, invID, adminID); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	form := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-05&due_date=2026-04-30&line_description=Fee&line_account_id=%d&line_quantity=1&line_unit_price=200000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices/"+strconv.Itoa(invID), cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (model error re-render), got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "can only edit draft invoices") {
		t.Errorf("expected body to surface the model error, got body of length %d", len(body))
	}
}

// --- PrintInvoice ------------------------------------------------------------

func TestPrintInvoice_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := seedCustomer(t, db, "Print Customer")
	revenueID := accountID(t, db, "4-1001")
	invID := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/print", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, "Latasya Transport") {
		t.Error("print view should render the company name")
	}
}

func TestPrintInvoice_InvalidID(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/notanumber/print", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPrintInvoice_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/9999999/print", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPrintInvoice_CompanyProfileMissing(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := seedCustomer(t, db, "No Profile Customer")
	revenueID := accountID(t, db, "4-1001")
	invID := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)

	if _, err := db.Exec("DELETE FROM company_profile"); err != nil {
		t.Fatalf("delete company profile: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/print", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

// --- InvoicePDF ---------------------------------------------------------------

func TestInvoicePDF_InvalidID(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/notanumber/pdf", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestInvoicePDF_NotFound(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/9999999/pdf", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestInvoicePDF_CompanyProfileMissing(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := seedCustomer(t, db, "PDF No Profile Customer")
	revenueID := accountID(t, db, "4-1001")
	invID := mustCreateDraftInvoice(t, db, contactID, revenueID, 100000)

	if _, err := db.Exec("DELETE FROM company_profile"); err != nil {
		t.Fatalf("delete company profile: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/pdf", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

// --- InvoiceLinePartial (HTMX) ------------------------------------------------

func TestInvoiceLinePartial_HTMX(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/htmx/invoice-line", cookies, "")
	req.Header.Set("HX-Request", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, `name="line_account_id"`) || !strings.Contains(body, "4-1001") {
		t.Errorf("expected invoice-line partial with a revenue account option, got body of length %d", len(body))
	}
}

// --- helpers -----------------------------------------------------------------

// flashValue returns the "flash" cookie's value set on resp, or "" if absent.
func flashValue(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == "flash" {
			return c.Value
		}
	}
	return ""
}
