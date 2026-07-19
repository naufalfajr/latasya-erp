package handler_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// publicTestServer wires only the unauthenticated public routes (home page,
// parent portal) — the admin auth chain isn't needed to exercise them.
func publicTestServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.PublicHome)
	mux.HandleFunc("GET /i/{token}", h.PortalIndex)
	mux.HandleFunc("GET /i/{token}/invoice/{id}/pdf", h.PortalInvoicePDF)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, db
}

// mustContact inserts a customer contact and returns its ID.
func mustContact(t *testing.T, db *sql.DB, name, phone string) int {
	t.Helper()
	if _, err := db.Exec(
		"INSERT INTO contacts (name, contact_type, phone, is_active) VALUES (?, 'customer', ?, 1)", name, phone,
	); err != nil {
		t.Fatalf("insert contact: %v", err)
	}
	var id int
	db.QueryRow("SELECT id FROM contacts WHERE name = ?", name).Scan(&id)
	return id
}

func TestPublicHome_ShowsCompanyProfile(t *testing.T) {
	ts, db := publicTestServer(t)
	db.Exec("UPDATE company_profile SET name='Latasya Transport', phone='081234567890' WHERE id=1")

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Latasya Transport") {
		t.Error("expected company name in homepage body")
	}
}

func TestPortalIndex_UnknownToken_ShowsInvalid(t *testing.T) {
	ts, _ := publicTestServer(t)

	resp, err := http.Get(ts.URL + "/i/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Link Tidak Valid") {
		t.Error("expected invalid-link message for an unknown token")
	}
}

func TestPortalIndex_ValidToken_ShowsIssuedInvoiceOnly(t *testing.T) {
	ts, db := publicTestServer(t)
	contactID := mustContact(t, db, "Portal Kid", "081111111111")

	draftID := mustInvoice(t, db, contactID)
	sentID := mustInvoice(t, db, contactID)
	if err := model.SendInvoice(db, sentID, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	token, err := model.GetOrCreatePortalToken(db, contactID)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}

	resp, err := http.Get(ts.URL + "/i/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	sentInv, _ := model.GetInvoice(db, sentID)
	if !strings.Contains(text, sentInv.InvoiceNumber) {
		t.Error("expected the sent invoice number in the portal page")
	}

	draftInv, _ := model.GetInvoice(db, draftID)
	if strings.Contains(text, draftInv.InvoiceNumber) {
		t.Error("draft invoice should not appear on the parent portal")
	}
}

func TestPortalIndex_ConfirmPaymentButton_HiddenWithoutCompanyPhone(t *testing.T) {
	ts, db := publicTestServer(t)
	db.Exec("UPDATE company_profile SET phone='' WHERE id=1")

	contactID := mustContact(t, db, "No Company Phone", "081111111111")
	invID := mustInvoice(t, db, contactID)
	if err := model.SendInvoice(db, invID, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	token, _ := model.GetOrCreatePortalToken(db, contactID)
	resp, err := http.Get(ts.URL + "/i/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if strings.Contains(text, "wa.me/?text") {
		t.Error("confirm-payment link should not be built with a blank company phone")
	}
	if strings.Contains(text, "Konfirmasi Sudah Bayar") {
		t.Error("confirm-payment button should be hidden when the company has no phone on file")
	}
}

func TestPortalInvoicePDF_WrongFamily_NotFound(t *testing.T) {
	ts, db := publicTestServer(t)
	contactA := mustContact(t, db, "Family A", "081111111111")
	contactB := mustContact(t, db, "Family B", "082222222222")

	invB := mustInvoice(t, db, contactB)
	if err := model.SendInvoice(db, invB, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	tokenA, _ := model.GetOrCreatePortalToken(db, contactA)

	resp, err := http.Get(ts.URL + "/i/" + tokenA + "/invoice/" + strconv.Itoa(invB) + "/pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 fetching another family's invoice PDF, got %d", resp.StatusCode)
	}
}

func TestPortalInvoicePDF_OwnInvoice_Succeeds(t *testing.T) {
	ts, db := publicTestServer(t)
	contactID := mustContact(t, db, "Portal PDF", "083333333333")
	invID := mustInvoice(t, db, contactID)
	if err := model.SendInvoice(db, invID, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	token, _ := model.GetOrCreatePortalToken(db, contactID)

	resp, err := http.Get(ts.URL + "/i/" + token + "/invoice/" + strconv.Itoa(invID) + "/pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("expected application/pdf, got %q", ct)
	}
}

// TestPortalPages_NoStore guards against caching an unauthenticated,
// durable financial-data URL: both the portal page and its PDF must tell
// shared caches/proxies not to store the response.
func TestPortalPages_NoStore(t *testing.T) {
	ts, db := publicTestServer(t)
	contactID := mustContact(t, db, "No Store", "085555555555")
	invID := mustInvoice(t, db, contactID)
	if err := model.SendInvoice(db, invID, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}
	token, _ := model.GetOrCreatePortalToken(db, contactID)

	indexResp, err := http.Get(ts.URL + "/i/" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer indexResp.Body.Close()
	if cc := indexResp.Header.Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("portal index: expected Cache-Control %q, got %q", "private, no-store", cc)
	}

	pdfResp, err := http.Get(ts.URL + "/i/" + token + "/invoice/" + strconv.Itoa(invID) + "/pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer pdfResp.Body.Close()
	if cc := pdfResp.Header.Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("portal PDF: expected Cache-Control %q, got %q", "private, no-store", cc)
	}
}

func TestInvoiceWhatsApp_Draft_RedirectsBackWithoutSending(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := mustContact(t, db, "WA Draft", "081111111111")
	invID := mustInvoice(t, db, contactID)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/whatsapp", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if strings.Contains(loc, "wa.me") {
		t.Errorf("draft invoice should not redirect to wa.me, got %q", loc)
	}
}

func TestInvoiceWhatsApp_NoPhone_RedirectsBackWithoutSending(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := mustContact(t, db, "WA NoPhone", "")
	invID := mustInvoice(t, db, contactID)
	if err := model.SendInvoice(db, invID, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/whatsapp", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if strings.Contains(loc, "wa.me") {
		t.Errorf("phoneless contact should not redirect to wa.me, got %q", loc)
	}
}

func TestInvoiceWhatsApp_Sent_RedirectsToWALink(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := mustContact(t, db, "WA Sent", "081234567890")
	invID := mustInvoice(t, db, contactID)
	if err := model.SendInvoice(db, invID, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/invoices/"+strconv.Itoa(invID)+"/whatsapp", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "https://wa.me/6281234567890") {
		t.Errorf("expected wa.me redirect for normalized phone, got %q", loc)
	}

	var token sql.NullString
	db.QueryRow("SELECT portal_token FROM contacts WHERE id = ?", contactID).Scan(&token)
	if !token.Valid || token.String == "" {
		t.Error("expected a portal token to be created for the contact")
	}
	if !strings.Contains(loc, token.String) {
		t.Error("expected the wa.me message to include the contact's portal link")
	}
}

// TestContactEditPage_ShowsPortalLinkControl guards the second bug found
// during manual browser review: the backend route for resetting a portal
// token existed and worked, but no template ever rendered a way to reach
// it, making the feature invisible in the actual UI.
func TestContactEditPage_ShowsPortalLinkControl(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := mustContact(t, db, "UI Reset", "081111111111")

	req, _ := requestWithCookies(db, "GET", ts.URL+"/contacts/"+strconv.Itoa(contactID)+"/edit", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !strings.Contains(text, "Buat Link Portal") {
		t.Error("expected a create-portal-link control before any token exists")
	}

	token, _ := model.GetOrCreatePortalToken(db, contactID)
	req2, _ := requestWithCookies(db, "GET", ts.URL+"/contacts/"+strconv.Itoa(contactID)+"/edit", cookies, "")
	resp2, err := noRedirectClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	text2 := string(body2)

	if !strings.Contains(text2, "Reset Link") {
		t.Error("expected a reset-link control once a token exists")
	}
	if !strings.Contains(text2, "/i/"+token) {
		t.Error("expected the current portal link to be displayed")
	}
}

func TestResetContactPortalToken_RegeneratesAndRedirects(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	contactID := mustContact(t, db, "Reset Me", "081111111111")

	oldToken, _ := model.GetOrCreatePortalToken(db, contactID)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/contacts/"+strconv.Itoa(contactID)+"/reset-token", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	wantLoc := "/contacts/" + strconv.Itoa(contactID) + "/edit"
	if loc := resp.Header.Get("Location"); loc != wantLoc {
		t.Errorf("expected redirect to %q, got %q", wantLoc, loc)
	}

	var newToken string
	db.QueryRow("SELECT portal_token FROM contacts WHERE id = ?", contactID).Scan(&newToken)
	if newToken == oldToken {
		t.Error("expected a new token after reset")
	}

	fam, err := model.ContactsByPortalToken(db, oldToken)
	if err != nil {
		t.Fatalf("lookup old token: %v", err)
	}
	if fam != nil {
		t.Error("old token should no longer resolve")
	}
}
