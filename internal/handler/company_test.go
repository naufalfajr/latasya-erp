package handler_test

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func testServerWithCompany(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /settings/company", auth.AdminOnly(h.CompanyProfilePage))
	protected.HandleFunc("POST /settings/company", auth.AdminOnly(h.UpdateCompanyProfile))
	protected.HandleFunc("GET /password/change", h.PasswordChangePage)
	protected.HandleFunc("POST /password/change", h.PasswordChange)

	mux.Handle("/", auth.RequireAuth(db, auth.CSRFProtect(handler.EnforcePasswordChange(protected))))

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

func TestCompanyProfilePage_AdminRenders(t *testing.T) {
	ts, db := testServerWithCompany(t)
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/settings/company", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Company Profile") {
		t.Error("body missing 'Company Profile'")
	}
	if !strings.Contains(string(body), "Latasya Transport") {
		t.Error("body missing seeded company name")
	}
}

func TestCompanyProfilePage_ViewerForbidden(t *testing.T) {
	ts, db := testServerWithCompany(t)
	cookies := loginAsViewer(t, ts, db)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/settings/company", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer, got %d", resp.StatusCode)
	}
}

func TestUpdateCompanyProfile_HTTP(t *testing.T) {
	ts, db := testServerWithCompany(t)
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"name":                {"PT Latasya Jaya"},
		"tagline":             {"Transport"},
		"address":             {"Jl. Mawar 1"},
		"bank_name":           {"BCA"},
		"bank_account_number": {"1234567890"},
		"bank_account_holder": {"PT Latasya Jaya"},
	}.Encode()

	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/company", cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}

	co, err := model.GetCompanyProfile(db)
	if err != nil {
		t.Fatalf("GetCompanyProfile: %v", err)
	}
	if co.Name != "PT Latasya Jaya" {
		t.Errorf("Name = %q, want %q", co.Name, "PT Latasya Jaya")
	}
	if co.BankAccountNumber != "1234567890" {
		t.Errorf("BankAccountNumber = %q, want %q", co.BankAccountNumber, "1234567890")
	}
}

func TestInvoicePDF_HTTP(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('PDF Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'PDF Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Sewa bus", Quantity: 100, UnitPrice: 1500000, AccountID: revenueID},
	})

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/pdf", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.HasPrefix(body, []byte("%PDF-1.")) {
		t.Error("response body is not a PDF")
	}
}
