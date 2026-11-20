package handler_test

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// invoiceOn creates a draft invoice dated date for contactID and sends it.
func invoiceOn(t *testing.T, db *sql.DB, contactID int, date string) int {
	t.Helper()
	var revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	id, err := testutil.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: date, DueDate: date, CreatedBy: 1,
	}, []model.InvoiceLine{{Description: "Antar jemput", Quantity: 100, UnitPrice: 400_000, AccountID: revenueID}})
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	if err := testutil.SendInvoice(db, id, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}
	return id
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// Each portal status keeps the ERP tone with an Indonesian label, and an
// unpaid family sees the payment card and a confirm button per open invoice.
func TestPortalIndex_StatusesAndPayment(t *testing.T) {
	t.Parallel()
	ts, db := publicTestServer(t)
	mustExec(t, db, "UPDATE company_profile SET phone = '081234567890', bank_name = 'BNI', bank_account_number = '2061281826', bank_account_holder = 'PT Contoh'")

	child := mustContact(t, db, "Status Family", "081200000010")
	invoiceOn(t, db, child, time.Now().Format("2006-01-02")) // sent, this month
	partial := invoiceOn(t, db, child, "2026-05-01")
	paid := invoiceOn(t, db, child, "2026-04-01")
	cancelled := invoiceOn(t, db, child, "2026-03-01")
	mustExec(t, db, "UPDATE invoices SET status = 'partial', amount_paid = 100000 WHERE id = ?", partial)
	mustExec(t, db, "UPDATE invoices SET status = 'paid', amount_paid = total WHERE id = ?", paid)
	// Cancelled means fully credited (creditnote.applyCredit), so nothing is due.
	mustExec(t, db, "UPDATE invoices SET status = 'cancelled', amount_credited = total WHERE id = ?", cancelled)
	code, err := testutil.GetOrCreatePortalCode(db, child)
	if err != nil {
		t.Fatal(err)
	}

	_, body := sendRequest(t, db, nil, "GET", ts.URL+"/p/"+code, "")
	for _, want := range []string{
		`asta-badge-accent"><span class="asta-badge-dot"></span>Belum lunas</span>`,
		`asta-badge-warning"><span class="asta-badge-dot"></span>Sebagian</span>`,
		`asta-badge-success"><span class="asta-badge-dot"></span>Lunas</span>`,
		`asta-badge-outline"><span class="asta-badge-dot"></span>Dibatalkan</span>`,
		"<dt>Lunas pada</dt>",
		"Cara pembayaran",
		`data-copy="2061281826"`,
		"Konfirmasi sudah bayar",
		"wa.me/",
		`data-copy="Status Family Mei 2026"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("portal missing %s", want)
		}
	}
	if strings.Contains(body, "Belum ada invoice bulan ini") {
		t.Error("portal says there is no invoice this month, but one is dated today")
	}
	if n := strings.Count(body, "Konfirmasi sudah bayar"); n != 2 {
		t.Errorf("confirm buttons: %d, want 2 (the sent and the partial invoice)", n)
	}
	if m := daisyClassIn(body); m != "" {
		t.Errorf("portal: DaisyUI class %q", m)
	}
}

// A fully paid family with nothing this month sees the notice and no
// payment card.
func TestPortalIndex_AllPaidNoCurrentMonth(t *testing.T) {
	t.Parallel()
	ts, db := publicTestServer(t)
	child := mustContact(t, db, "Paid Family", "081200000011")
	paid := invoiceOn(t, db, child, "2020-01-01")
	mustExec(t, db, "UPDATE invoices SET status = 'paid', amount_paid = total WHERE id = ?", paid)
	code, _ := testutil.GetOrCreatePortalCode(db, child)

	_, body := sendRequest(t, db, nil, "GET", ts.URL+"/p/"+code, "")
	if !strings.Contains(body, "Belum ada invoice bulan ini.") {
		t.Error("expected the no-invoice-this-month notice")
	}
	for _, unwanted := range []string{"Cara pembayaran", "Konfirmasi sudah bayar", "Berita transfer"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("fully paid family should not see %q", unwanted)
		}
	}
}

// A failed sign-in shows the error in a danger alert and keeps the username.
func TestLogin_BadPassword_ShowsAlertKeepsUsername(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	_, body := sendRequest(t, db, nil, "POST", ts.URL+"/login", "username=admin&password=wrong")
	if !strings.Contains(body, `<div class="asta-alert asta-alert-danger mb-4" role="alert">`) || !strings.Contains(body, "Invalid username or password") {
		t.Error("expected the error in a danger alert")
	}
	if !strings.Contains(body, `value="admin"`) {
		t.Error("expected the username to be kept")
	}
	assertAstaPage(t, "login error", body, true)
}

// Approving with no capability re-renders the consent page with the field
// error and keeps the client's request intact.
func TestOAuthAuthorize_NoScopesRerendersWithError(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	authorize := ts.URL + oauthAuthorizeURL("http://127.0.0.1:59123/callback", oauthChallenge(t))

	_, page := sendRequest(t, db, cookies, "GET", authorize, "")
	form := url.Values{"csrf_token": {csrfFromHTML(page)}, "action": {"approve"}}
	post, _ := http.NewRequest("POST", authorize, strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		post.AddCookie(c)
	}
	resp, err := noRedirectClient().Do(post)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	body := string(b)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200 with the form again", resp.StatusCode)
	}
	if !strings.Contains(body, `<p class="asta-error" role="alert">Select at least one capability.</p>`) {
		t.Error("expected the scopes field error")
	}
	if !strings.Contains(body, `name="scopes" value="`+model.CapReportsView+`"`) {
		t.Error("expected the capability checkboxes to render again")
	}
	assertAstaPage(t, "consent error", body, true)
}

// The printed invoice carries every receipt section the data has.
func TestPrintInvoice_FullReceipt(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)
	mustExec(t, db, "UPDATE company_profile SET bank_name = 'BNI', bank_account_number = '2061281826', bank_account_holder = 'PT Contoh', invoice_footer = 'Terima kasih.'")
	contact := mustContact(t, db, "Receipt Family", "081200000012")
	inv := invoiceOn(t, db, contact, "2026-06-01")
	mustExec(t, db, "UPDATE invoices SET tax_amount = 11000, amount_paid = 100000, status = 'partial', notes = 'Bayar sebelum tanggal 10.' WHERE id = ?", inv)

	_, body := sendRequest(t, db, cookies, "GET", fmt.Sprintf("%s/invoices/%d/print", ts.URL, inv), "")
	for _, want := range []string{
		"<td>Tax</td>", "<td>Paid</td>", "<td>Balance due</td>",
		"<label>Notes</label>", "Bayar sebelum tanggal 10.",
		"<label>Payment</label>", "BNI — 2061281826 (a.n. PT Contoh)",
		`<div class="footer">Terima kasih.</div>`,
		`<p class="mono">INV-`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("print missing %s", want)
		}
	}
}

// Without bank details on file, the payment card keeps the total and the
// transfer-note advice but drops the empty account block and its copy button.
func TestPortalIndex_NoBankDetails_HidesAccountBlock(t *testing.T) {
	t.Parallel()
	ts, db := publicTestServer(t)
	mustExec(t, db, "UPDATE company_profile SET bank_name = '', bank_account_number = '', bank_account_holder = ''")
	child := mustContact(t, db, "No Bank Family", "081200000013")
	invoiceOn(t, db, child, "2026-06-01")
	code, _ := testutil.GetOrCreatePortalCode(db, child)

	_, body := sendRequest(t, db, nil, "GET", ts.URL+"/p/"+code, "")
	if !strings.Contains(body, "Cara pembayaran") {
		t.Fatal("expected the payment card for an unpaid family")
	}
	if strings.Contains(body, `data-copy=""`) || strings.Contains(body, "a.n.") {
		t.Error("payment card shows an empty bank account block")
	}
}
