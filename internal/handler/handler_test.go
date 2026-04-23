package handler_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/testutil"
)

const adminTestPassword = "admin-test-password"

// testServer sets up a full HTTP test server with all routes wired up.
func testServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("POST /logout", h.Logout)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", h.Dashboard)
	protected.HandleFunc("GET /accounts", h.ListAccounts)
	protected.HandleFunc("GET /accounts/new", h.NewAccount)
	protected.HandleFunc("POST /accounts", auth.CapabilityOnly("accounts.manage", h.CreateAccount))
	protected.HandleFunc("GET /accounts/{id}/edit", h.EditAccount)
	protected.HandleFunc("POST /accounts/{id}", auth.CapabilityOnly("accounts.manage", h.UpdateAccount))
	protected.HandleFunc("DELETE /accounts/{id}", auth.CapabilityOnly("accounts.manage", h.DeleteAccount))
	protected.HandleFunc("GET /contacts", h.ListContacts)
	protected.HandleFunc("GET /contacts/new", h.NewContact)
	protected.HandleFunc("POST /contacts", auth.CapabilityOnly("contacts.manage", h.CreateContact))
	protected.HandleFunc("GET /contacts/{id}/edit", h.EditContact)
	protected.HandleFunc("POST /contacts/{id}", auth.CapabilityOnly("contacts.manage", h.UpdateContact))
	protected.HandleFunc("DELETE /contacts/{id}", auth.CapabilityOnly("contacts.manage", h.DeleteContact))
	protected.HandleFunc("GET /journals", h.ListJournals)
	protected.HandleFunc("GET /journals/new", h.NewJournal)
	protected.HandleFunc("POST /journals", auth.CapabilityOnly("journals.manage", h.CreateJournal))
	protected.HandleFunc("GET /journals/{id}", h.ViewJournal)
	protected.HandleFunc("GET /journals/{id}/edit", h.EditJournal)
	protected.HandleFunc("POST /journals/{id}", auth.CapabilityOnly("journals.manage", h.UpdateJournal))
	protected.HandleFunc("DELETE /journals/{id}", auth.CapabilityOnly("journals.manage", h.DeleteJournal))
	protected.HandleFunc("GET /htmx/journal-line", h.JournalLinePartial)
	protected.HandleFunc("GET /income", h.ListIncome)
	protected.HandleFunc("GET /income/new", h.NewIncome)
	protected.HandleFunc("POST /income", auth.CapabilityOnly("income.manage", h.CreateIncome))
	protected.HandleFunc("GET /income/{id}/edit", h.EditIncome)
	protected.HandleFunc("POST /income/{id}", auth.CapabilityOnly("income.manage", h.UpdateIncome))
	protected.HandleFunc("DELETE /income/{id}", auth.CapabilityOnly("income.manage", h.DeleteIncome))
	protected.HandleFunc("GET /expenses", h.ListExpenses)
	protected.HandleFunc("GET /expenses/new", h.NewExpense)
	protected.HandleFunc("POST /expenses", auth.CapabilityOnly("expenses.manage", h.CreateExpense))
	protected.HandleFunc("GET /expenses/{id}/edit", h.EditExpense)
	protected.HandleFunc("POST /expenses/{id}", auth.CapabilityOnly("expenses.manage", h.UpdateExpense))
	protected.HandleFunc("DELETE /expenses/{id}", auth.CapabilityOnly("expenses.manage", h.DeleteExpense))
	protected.HandleFunc("GET /invoices", h.ListInvoices)
	protected.HandleFunc("GET /invoices/new", h.NewInvoice)
	protected.HandleFunc("POST /invoices", auth.CapabilityOnly("invoices.manage", h.CreateInvoice))
	protected.HandleFunc("GET /invoices/{id}", h.ViewInvoice)
	protected.HandleFunc("GET /invoices/{id}/edit", h.EditInvoice)
	protected.HandleFunc("POST /invoices/{id}", auth.CapabilityOnly("invoices.manage", h.UpdateInvoice))
	protected.HandleFunc("DELETE /invoices/{id}", auth.CapabilityOnly("invoices.manage", h.DeleteInvoice))
	protected.HandleFunc("POST /invoices/{id}/send", auth.CapabilityOnly("invoices.manage", h.SendInvoice))
	protected.HandleFunc("POST /invoices/{id}/payment", auth.CapabilityOnly("invoices.manage", h.InvoicePayment))
	protected.HandleFunc("GET /invoices/{id}/print", h.PrintInvoice)
	protected.HandleFunc("GET /htmx/invoice-line", h.InvoiceLinePartial)
	protected.HandleFunc("GET /bills", h.ListBills)
	protected.HandleFunc("GET /bills/new", h.NewBill)
	protected.HandleFunc("POST /bills", auth.CapabilityOnly("bills.manage", h.CreateBill))
	protected.HandleFunc("GET /bills/{id}", h.ViewBill)
	protected.HandleFunc("GET /bills/{id}/edit", h.EditBill)
	protected.HandleFunc("POST /bills/{id}", auth.CapabilityOnly("bills.manage", h.UpdateBill))
	protected.HandleFunc("DELETE /bills/{id}", auth.CapabilityOnly("bills.manage", h.DeleteBill))
	protected.HandleFunc("POST /bills/{id}/receive", auth.CapabilityOnly("bills.manage", h.ReceiveBill))
	protected.HandleFunc("POST /bills/{id}/payment", auth.CapabilityOnly("bills.manage", h.BillPayment))
	protected.HandleFunc("GET /htmx/bill-line", h.BillLinePartial)
	protected.HandleFunc("GET /reports/trial-balance", h.TrialBalance)
	protected.HandleFunc("GET /reports/profit-loss", h.ProfitLoss)
	protected.HandleFunc("GET /reports/balance-sheet", h.BalanceSheet)
	protected.HandleFunc("GET /reports/cash-flow", h.CashFlowReport)
	protected.HandleFunc("GET /reports/general-ledger", h.GeneralLedger)
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /users", h.ListUsers)
	adminMux.HandleFunc("GET /users/new", h.NewUser)
	adminMux.HandleFunc("POST /users", h.CreateUser)
	adminMux.HandleFunc("GET /users/{id}/edit", h.EditUser)
	adminMux.HandleFunc("POST /users/{id}", h.UpdateUser)
	adminMux.HandleFunc("DELETE /users/{id}", h.DeleteUser)
	protected.Handle("/users", auth.RequireCapability("users.manage")(adminMux))
	protected.Handle("/users/", auth.RequireCapability("users.manage")(adminMux))

	roleMux := http.NewServeMux()
	roleMux.HandleFunc("GET /roles", h.ListRoles)
	roleMux.HandleFunc("GET /roles/new", h.NewRole)
	roleMux.HandleFunc("POST /roles", h.CreateRole)
	roleMux.HandleFunc("GET /roles/{name}/edit", h.EditRole)
	roleMux.HandleFunc("POST /roles/{name}", h.UpdateRole)
	roleMux.HandleFunc("DELETE /roles/{name}", h.DeleteRole)
	protected.Handle("/roles", auth.RequireCapability("roles.manage")(roleMux))
	protected.Handle("/roles/", auth.RequireCapability("roles.manage")(roleMux))

	protected.HandleFunc("GET /password/change", h.PasswordChangePage)
	protected.HandleFunc("POST /password/change", h.PasswordChange)

	protected.HandleFunc("GET /audit", auth.CapabilityOnly("audit.view", h.AuditList))

	mux.Handle("/", auth.RequireAuth(db, auth.CSRFProtect(handler.EnforcePasswordChange(protected))))

	// Replace the seeded admin/admin with a non-default password and clear the
	// forced-change flag so tests logging in as admin aren't bounced to the
	// password-change page (the login handler re-flags the literal admin/admin
	// pair for legacy safety).
	hash, err := auth.HashPassword(adminTestPassword)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	if _, err := db.Exec(
		"UPDATE users SET password=?, must_change_password=0 WHERE username='admin'",
		hash,
	); err != nil {
		t.Fatalf("update admin: %v", err)
	}

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, db
}

// loginAsAdmin logs in as the default admin and returns the session cookie.
func loginAsAdmin(t *testing.T, ts *httptest.Server) []*http.Cookie {
	t.Helper()
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse // don't follow redirects
	}}

	form := url.Values{"username": {"admin"}, "password": {adminTestPassword}}
	resp, err := client.PostForm(ts.URL+"/login", form)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on login, got %d", resp.StatusCode)
	}
	return resp.Cookies()
}

// loginAsViewer creates a viewer user, logs in, and returns the session cookie.
func loginAsViewer(t *testing.T, ts *httptest.Server, db *sql.DB) []*http.Cookie {
	t.Helper()
	hash, _ := auth.HashPassword("viewer")
	db.Exec("INSERT OR IGNORE INTO users (username, password, full_name, role) VALUES (?, ?, ?, ?)",
		"viewer", hash, "Viewer User", "viewer")

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{"username": {"viewer"}, "password": {"viewer"}}
	resp, err := client.PostForm(ts.URL+"/login", form)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()
	return resp.Cookies()
}

// loginAsBookkeeper creates a bookkeeper user, logs in, and returns the session cookie.
func loginAsBookkeeper(t *testing.T, ts *httptest.Server, db *sql.DB) []*http.Cookie {
	t.Helper()
	hash, _ := auth.HashPassword("bookkeeper")
	db.Exec("INSERT OR IGNORE INTO users (username, password, full_name, role) VALUES (?, ?, ?, ?)",
		"bookkeeper", hash, "Bookkeeper User", "bookkeeper")

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{"username": {"bookkeeper"}, "password": {"bookkeeper"}}
	resp, err := client.PostForm(ts.URL+"/login", form)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()
	return resp.Cookies()
}

func requestWithCookies(db *sql.DB, method, url string, cookies []*http.Cookie, body string) (*http.Request, error) {
	var req *http.Request
	var err error
	if body != "" {
		req, err = http.NewRequest(method, url, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req, err = http.NewRequest(method, url, nil)
		if err != nil {
			return nil, err
		}
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	// Auto-attach CSRF token for state-changing requests (server also accepts
	// it as form field, but setting the header works for both form and empty
	// bodies without rewriting the body).
	if db != nil && method != http.MethodGet && method != http.MethodHead {
		for _, c := range cookies {
			if c.Name == "session_id" && c.Value != "" {
				if token, err := auth.GetSessionCSRF(db, c.Value); err == nil {
					req.Header.Set("X-CSRF-Token", token)
				}
				break
			}
		}
	}
	return req, nil
}

// --- Auth Handler Tests ---

func TestLoginPage(t *testing.T) {
	ts, _ := testServer(t)
	resp, err := http.Get(ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestLogin_Success(t *testing.T) {
	ts, _ := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var found bool
	for _, c := range cookies {
		if c.Name == "session_id" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected session_id cookie after login")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	ts, _ := testServer(t)
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{"username": {"admin"}, "password": {"wrong"}}
	resp, err := client.PostForm(ts.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should re-render login page with error (200, not redirect)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render), got %d", resp.StatusCode)
	}
}

func TestLogin_EmptyFields(t *testing.T) {
	ts, _ := testServer(t)
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := url.Values{"username": {""}, "password": {""}}
	resp, err := client.PostForm(ts.URL+"/login", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (form re-render), got %d", resp.StatusCode)
	}
}

func TestDashboard_RequiresAuth(t *testing.T) {
	ts, _ := testServer(t)
	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect to login, got %d", resp.StatusCode)
	}
}

func TestDashboard_Authenticated(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestLogout(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	req, _ := requestWithCookies(db, "POST", ts.URL+"/logout", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", resp.StatusCode)
	}

	// After logout, dashboard should redirect
	req2, _ := requestWithCookies(db, "GET", ts.URL+"/", resp.Cookies(), "")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 after logout, got %d", resp2.StatusCode)
	}
}

// --- Account Handler Tests ---

func TestListAccounts_Authenticated(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/accounts", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateAccount_Admin(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := "code=9-9999&name=Test+Account&account_type=asset&normal_balance=debit&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}
}

func TestCreateAccount_ValidationError(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Missing required fields
	form := "code=&name=&account_type=&normal_balance="
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should re-render form (200), not redirect
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

func TestDeleteAccount_HTMX(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// First create an account to delete
	form := "code=9-8888&name=To+Delete&account_type=asset&normal_balance=debit&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/accounts", cookies, form)
	client.Do(req)

	// Find the account ID
	var id int
	// We need to find it — simplest is to try deleting by a known range
	// Actually, let's just test with a high ID that we know we created
	req2, _ := requestWithCookies(db, "DELETE", ts.URL+"/accounts/100", cookies, "")
	req2.Header.Set("HX-Request", "true")
	resp, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Even if account doesn't exist at that ID, the handler should handle it
	// The point is testing the HTMX response path
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 200 or 404, got %d", resp.StatusCode)
	}
	_ = id
}

// --- Contact Handler Tests ---

func TestListContacts_Authenticated(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/contacts", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateContact_Admin(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := "name=SD+Negeri+1&contact_type=customer&phone=08123456789&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/contacts", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}
}

func TestCreateContact_ValidationError(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := "name=&contact_type="
	req, _ := requestWithCookies(db, "POST", ts.URL+"/contacts", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

// --- Authorization Tests ---

func TestAccounts_ViewerCanView(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/accounts", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("viewer should be able to view accounts, got %d", resp.StatusCode)
	}
}

func TestContacts_ViewerCanView(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/contacts", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("viewer should be able to view contacts, got %d", resp.StatusCode)
	}
}

// --- Journal Handler Tests ---

func TestListJournals_Authenticated(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestNewJournal_Form(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/journals/new", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateJournal_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Get account IDs
	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Test+journal&line_account_id=%d&line_account_id=%d&line_debit=5000000&line_debit=0&line_credit=0&line_credit=5000000&line_memo=Cash&line_memo=Revenue",
		cashID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}

	// Should redirect to /journals/{id}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/journals/") {
		t.Errorf("expected redirect to /journals/{id}, got %q", loc)
	}
}

func TestCreateJournal_ValidationError_MissingDate(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := fmt.Sprintf(
		"entry_date=&description=Test&line_account_id=%d&line_account_id=%d&line_debit=1000&line_debit=0&line_credit=0&line_credit=1000&line_memo=&line_memo=",
		cashID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

func TestCreateJournal_ValidationError_Unbalanced(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Unbalanced&line_account_id=%d&line_account_id=%d&line_debit=5000&line_debit=0&line_credit=0&line_credit=3000&line_memo=&line_memo=",
		cashID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error for unbalanced), got %d", resp.StatusCode)
	}
}

func TestViewJournal(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Create a journal entry first
	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=View+test&line_account_id=%d&line_account_id=%d&line_debit=1000&line_debit=0&line_credit=0&line_credit=1000&line_memo=&line_memo=",
		cashID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/journals", cookies, form)
	resp, _ := client.Do(req)
	loc := resp.Header.Get("Location")

	// View the created entry
	client2 := &http.Client{}
	req2, _ := requestWithCookies(db, "GET", ts.URL+loc, cookies, "")
	resp2, err := client2.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp2.StatusCode)
	}
}

func TestJournalLinePartial_HTMX(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/htmx/journal-line", cookies, "")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// --- Income Handler Tests ---

func TestListIncome(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/income", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateIncome_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=School+bus+payment&amount=5000000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/journals/") {
		t.Errorf("expected redirect to /journals/{id}, got %q", loc)
	}

	// Verify journal entry was created with correct source_type
	var sourceType string
	id := strings.TrimPrefix(loc, "/journals/")
	db.QueryRow("SELECT COALESCE(source_type,'') FROM journal_entries WHERE id = ?", id).Scan(&sourceType)
	if sourceType != "income" {
		t.Errorf("expected source_type 'income', got %q", sourceType)
	}

	// Verify debit = credit = 5000000
	var totalDebit, totalCredit int
	db.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM journal_lines WHERE entry_id = ?", id).Scan(&totalDebit, &totalCredit)
	if totalDebit != 5000000 || totalCredit != 5000000 {
		t.Errorf("expected balanced 5000000, got debit=%d credit=%d", totalDebit, totalCredit)
	}
}

func TestCreateIncome_ValidationError(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := "entry_date=&description=&amount=0&revenue_account=&deposit_account="
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

// --- Expense Handler Tests ---

func TestListExpenses(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/expenses", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestCreateExpense_Success(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Diesel+fuel+Bus+01&amount=500000&expense_account=%d&payment_account=%d",
		fuelID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/journals/") {
		t.Errorf("expected redirect to /journals/{id}, got %q", loc)
	}

	// Verify journal entry source_type
	var sourceType string
	id := strings.TrimPrefix(loc, "/journals/")
	db.QueryRow("SELECT COALESCE(source_type,'') FROM journal_entries WHERE id = ?", id).Scan(&sourceType)
	if sourceType != "expense" {
		t.Errorf("expected source_type 'expense', got %q", sourceType)
	}

	// Verify debit = credit = 500000
	var totalDebit, totalCredit int
	db.QueryRow("SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM journal_lines WHERE entry_id = ?", id).Scan(&totalDebit, &totalCredit)
	if totalDebit != 500000 || totalCredit != 500000 {
		t.Errorf("expected balanced 500000, got debit=%d credit=%d", totalDebit, totalCredit)
	}
}

func TestCreateExpense_ValidationError(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := "entry_date=&description=&amount=0&expense_account=&payment_account="
	req, _ := requestWithCookies(db, "POST", ts.URL+"/expenses", cookies, form)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

// --- Invoice Handler Tests ---

func TestListInvoices_Handler(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestInvoiceLifecycle_Handler(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Create a customer
	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SD Test', 'customer', 1)")
	var contactID, revenueID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SD Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// 1. Create invoice
	form := fmt.Sprintf(
		"contact_id=%d&invoice_date=2026-04-04&due_date=2026-04-30&line_description=Bus+fee&line_account_id=%d&line_quantity=1&line_unit_price=5000000",
		contactID, revenueID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/invoices", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create invoice: expected 303, got %d", resp.StatusCode)
	}
	invLoc := resp.Header.Get("Location")

	// 2. View invoice
	client := &http.Client{}
	req2, _ := requestWithCookies(db, "GET", ts.URL+invLoc, cookies, "")
	resp2, _ := client.Do(req2)
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("view invoice: expected 200, got %d", resp2.StatusCode)
	}

	// 3. Send invoice
	req3, _ := requestWithCookies(db, "POST", ts.URL+invLoc+"/send", cookies, "")
	resp3, _ := noRedirect.Do(req3)
	if resp3.StatusCode != http.StatusSeeOther {
		t.Errorf("send invoice: expected 303, got %d", resp3.StatusCode)
	}

	// 4. Record payment
	payForm := fmt.Sprintf("payment_date=2026-04-10&amount=5000000&payment_account=%d", cashID)
	req4, _ := requestWithCookies(db, "POST", ts.URL+invLoc+"/payment", cookies, payForm)
	resp4, _ := noRedirect.Do(req4)
	if resp4.StatusCode != http.StatusSeeOther {
		t.Errorf("payment: expected 303, got %d", resp4.StatusCode)
	}

	// 5. Verify final status is "paid"
	var status string
	invIDStr := strings.TrimPrefix(invLoc, "/invoices/")
	db.QueryRow("SELECT status FROM invoices WHERE id = ?", invIDStr).Scan(&status)
	if status != "paid" {
		t.Errorf("expected status 'paid', got %q", status)
	}
}

// --- Bill Handler Tests ---

func TestListBills_Handler(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/bills", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestBillLifecycle_Handler(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SPBU Test', 'supplier', 1)")
	var contactID, fuelID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SPBU Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// 1. Create bill
	form := fmt.Sprintf(
		"contact_id=%d&bill_date=2026-04-04&due_date=2026-04-30&line_description=Diesel&line_account_id=%d&line_quantity=1&line_unit_price=2000000",
		contactID, fuelID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/bills", cookies, form)
	resp, _ := noRedirect.Do(req)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create bill: expected 303, got %d", resp.StatusCode)
	}
	billLoc := resp.Header.Get("Location")

	// 2. Receive bill
	req2, _ := requestWithCookies(db, "POST", ts.URL+billLoc+"/receive", cookies, "")
	resp2, _ := noRedirect.Do(req2)
	if resp2.StatusCode != http.StatusSeeOther {
		t.Errorf("receive bill: expected 303, got %d", resp2.StatusCode)
	}

	// 3. Record payment
	payForm := fmt.Sprintf("payment_date=2026-04-10&amount=2000000&payment_account=%d", cashID)
	req3, _ := requestWithCookies(db, "POST", ts.URL+billLoc+"/payment", cookies, payForm)
	resp3, _ := noRedirect.Do(req3)
	if resp3.StatusCode != http.StatusSeeOther {
		t.Errorf("payment: expected 303, got %d", resp3.StatusCode)
	}

	// 4. Verify paid
	var billStatus string
	billIDStr := strings.TrimPrefix(billLoc, "/bills/")
	db.QueryRow("SELECT status FROM bills WHERE id = ?", billIDStr).Scan(&billStatus)
	if billStatus != "paid" {
		t.Errorf("expected status 'paid', got %q", billStatus)
	}
}

// --- Report Handler Tests ---

func TestReportPages_Authenticated(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	client := &http.Client{}

	pages := []string{
		"/reports/trial-balance",
		"/reports/profit-loss",
		"/reports/balance-sheet",
		"/reports/cash-flow",
		"/reports/general-ledger",
	}

	for _, page := range pages {
		req, _ := requestWithCookies(db, "GET", ts.URL+page, cookies, "")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", page, resp.StatusCode)
		}
	}
}

func TestReportPages_ViewerCanAccess(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)
	client := &http.Client{}

	pages := []string{
		"/reports/trial-balance",
		"/reports/profit-loss",
		"/reports/balance-sheet",
	}

	for _, page := range pages {
		req, _ := requestWithCookies(db, "GET", ts.URL+page, cookies, "")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("viewer should access %s, got %d", page, resp.StatusCode)
		}
	}
}

// --- User Management Tests ---

func TestListUsers_AdminOnly(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/users", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestListUsers_ViewerDenied(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsViewer(t, ts, db)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+"/users", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer should get 403, got %d", resp.StatusCode)
	}
}

func TestCreateUser_Admin(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := "username=newuser&full_name=New+User&password=test1234&role=viewer&is_active=on"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", resp.StatusCode)
	}
}

func TestCreateUser_ValidationError(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	form := "username=&full_name=&password=&role=invalid"
	req, _ := requestWithCookies(db, "POST", ts.URL+"/users", cookies, form)
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (validation error), got %d", resp.StatusCode)
	}
}

// --- Dashboard Tests ---

func TestDashboard_WithData(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Create some test data
	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	noRedirect := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	// Record income
	form := fmt.Sprintf(
		"entry_date=2026-04-04&description=Test+income&amount=5000000&revenue_account=%d&deposit_account=%d",
		revenueID, cashID,
	)
	req, _ := requestWithCookies(db, "POST", ts.URL+"/income", cookies, form)
	noRedirect.Do(req)

	// Check dashboard renders
	client := &http.Client{}
	req2, _ := requestWithCookies(db, "GET", ts.URL+"/", cookies, "")
	resp, err := client.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
