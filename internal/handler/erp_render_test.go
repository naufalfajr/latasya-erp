package handler_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// erpFixture seeds one record of each kind so detail pages render real data.
type erpFixture struct {
	contact, draftInvoice, sentInvoice, bill, creditNote, journal int
}

// newERPFixture starts a signed-in admin session over seeded data and returns
// a GET helper for ERP paths.
func newERPFixture(t *testing.T) (erpFixture, func(method, path string) (*http.Response, string)) {
	t.Helper()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var f erpFixture
	f.contact = mustContact(t, db, "Render Family", "081200000001")
	f.draftInvoice = mustInvoice(t, db, f.contact)
	f.sentInvoice = mustInvoice(t, db, f.contact)
	if err := testutil.SendInvoice(db, f.sentInvoice, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}
	if err := db.QueryRow("SELECT journal_id FROM invoices WHERE id = ?", f.sentInvoice).Scan(&f.journal); err != nil {
		t.Fatalf("journal of sent invoice: %v", err)
	}
	var revenue, expense int
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenue)
	db.QueryRow("SELECT id FROM accounts WHERE account_type = 'expense' ORDER BY code LIMIT 1").Scan(&expense)
	if _, err := db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Render Supplier', 'supplier', 1)"); err != nil {
		t.Fatalf("insert supplier: %v", err)
	}
	var supplier int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Render Supplier'").Scan(&supplier)
	var err error
	f.bill, err = testutil.CreateBill(db, &model.Bill{ContactID: supplier, BillDate: "2026-08-01", DueDate: "2026-08-31", CreatedBy: 1},
		[]model.BillLine{{Description: "Render bill", Quantity: 100, UnitPrice: 100_000, AccountID: expense}})
	if err != nil {
		t.Fatalf("create bill: %v", err)
	}
	f.creditNote, err = testutil.CreateCreditNote(db, &model.CreditNote{
		ContactID: f.contact, CNDate: "2026-06-05", Reason: model.CreditNoteReasonOther, CreatedBy: 1,
	}, []model.CreditNoteLine{{Description: "Render credit", Quantity: 100, UnitPrice: 50_000, AccountID: revenue}})
	if err != nil {
		t.Fatalf("create credit note: %v", err)
	}

	get := func(method, path string) (*http.Response, string) {
		return sendRequest(t, db, cookies, method, ts.URL+path, "")
	}
	return f, get
}

func erpRoutes(f erpFixture) []string {
	return []string{
		"/", "/?granularity=quarterly",
		"/income", "/income/new", "/expenses", "/expenses/new",
		"/journals", "/journals/new", fmt.Sprintf("/journals/%d", f.journal),
		"/invoices", "/invoices?status=draft", "/invoices/new",
		fmt.Sprintf("/invoices/%d", f.draftInvoice), fmt.Sprintf("/invoices/%d", f.sentInvoice), fmt.Sprintf("/invoices/%d/edit", f.draftInvoice),
		"/credit-notes", "/credit-notes/new", fmt.Sprintf("/credit-notes/%d", f.creditNote),
		"/bills", "/bills/new", fmt.Sprintf("/bills/%d", f.bill),
		"/contacts", "/contacts/new", fmt.Sprintf("/contacts/%d/edit", f.contact),
		"/reports/profit-loss", "/reports/balance-sheet", "/reports/cash-flow", "/reports/trial-balance", "/reports/general-ledger",
		"/accounts", "/accounts/new",
		"/settings/company", "/settings/school-calendar",
		"/users", "/users/new", "/roles", "/roles/new", "/audit",
		"/password/change", "/settings/api-tokens", "/settings/api-tokens/new",
	}
}

var (
	renderedClass = regexp.MustCompile(`class="([^"]*)"`)
	// Same component list as templates_test.go, applied to rendered HTML.
	daisyToken = regexp.MustCompile(`^(btn|card|card-body|card-title|card-actions|badge|alert|input-bordered|select-bordered|textarea-bordered|form-control|label-text|tab|tabs|tab-active|join|join-item|stat|stats|stat-title|stat-value|divider|drawer|drawer-side|drawer-toggle|menu|menu-title|navbar|link|link-hover|link-primary|checkbox|toggle|loading|rounded-box)(-[a-z0-9]+)*$|^(bg|text|border)-(base|primary|secondary|accent|neutral|info|success|warning|error)(-[a-z0-9]+)?$`)
)

// daisyClassIn returns the first DaisyUI class token in body, comparing whole
// tokens so kit names like asta-btn do not match.
func daisyClassIn(body string) string {
	for _, m := range renderedClass.FindAllStringSubmatch(body, -1) {
		for _, cls := range strings.Fields(m[1]) {
			if daisyToken.MatchString(cls[strings.LastIndex(cls, ":")+1:]) {
				return cls
			}
		}
	}
	return ""
}

// Every signed-in ERP screen renders inside the Astabyte shell, with no
// DaisyUI left in the page and the theme partial loaded.
func TestERPPages_RenderOnAstaShell(t *testing.T) {
	t.Parallel()
	f, get := newERPFixture(t)
	for _, path := range erpRoutes(f) {
		resp, body := get("GET", path)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d", path, resp.StatusCode)
			continue
		}
		for _, want := range []string{`<body class="latasya"`, `class="asta-sidebar erp-sidebar"`, `id="erp-nav"`, `onclick="toggleTheme()"`, `/static/css/asta/asta.js`} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s: missing %s", path, want)
			}
		}
		if m := daisyClassIn(body); m != "" {
			t.Errorf("GET %s: DaisyUI class in rendered page: %s", path, m)
		}
		if strings.Contains(body, `data-theme="silk"`) {
			t.Errorf("GET %s: signed-in page still opts into the DaisyUI theme", path)
		}
	}
}

// The sidebar marks the current section on its index and on pages under it,
// in both the desktop sidebar and the phone drawer.
func TestSidebar_MarksCurrentSection(t *testing.T) {
	t.Parallel()
	f, get := newERPFixture(t)
	for path, section := range map[string]string{
		"/":         "/",
		"/invoices": "/invoices",
		fmt.Sprintf("/invoices/%d", f.sentInvoice):  "/invoices",
		"/reports/general-ledger":                   "/reports/general-ledger",
		fmt.Sprintf("/contacts/%d/edit", f.contact): "/contacts",
	} {
		_, body := get("GET", path)
		current := regexp.MustCompile(`<a href="([^"]*)" class="asta-sidebar-link" aria-current="page">`).FindAllStringSubmatch(body, -1)
		if len(current) != 2 {
			t.Errorf("GET %s: %d current sidebar links, want 2 (sidebar + drawer)", path, len(current))
			continue
		}
		for _, c := range current {
			if c[1] != section {
				t.Errorf("GET %s: current link %q, want %q", path, c[1], section)
			}
		}
	}
}

// Phone layouts hide columns, so the actions those columns held must be
// repeated in the visible cell: every contact stays deletable on a phone.
func TestContactsList_DeleteReachableAtEveryWidth(t *testing.T) {
	t.Parallel()
	f, get := newERPFixture(t)
	_, body := get("GET", "/contacts")
	del := fmt.Sprintf(`hx-delete="/contacts/%d"`, f.contact)
	if n := strings.Count(body, del); n != 2 {
		t.Fatalf("contact delete controls: %d, want 2 (desktop column + phone cell)", n)
	}
	if !strings.Contains(body, `asta-btn asta-btn-outline erp-danger asta-btn-xs mt-1.5 sm:hidden"`) {
		t.Error("phone delete control missing its sm:hidden class")
	}
}

// Status badges render as sentence-case labels, never raw enum values.
func TestInvoiceList_StatusBadgesHumanized(t *testing.T) {
	t.Parallel()
	_, get := newERPFixture(t)
	_, body := get("GET", "/invoices")
	for _, want := range []string{`<span class="asta-badge-dot"></span>Draft</span>`, `<span class="asta-badge-dot"></span>Sent</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("invoices list missing badge %s", want)
		}
	}
	if strings.Contains(body, `</span>draft</span>`) || strings.Contains(body, `</span>sent</span>`) {
		t.Error("invoices list shows a raw lowercase status")
	}
}

// The phone meta line under a contact name joins only the fields present,
// so it never starts with a separator, and keeps the map link reachable.
func TestContactsList_PhoneMetaLine(t *testing.T) {
	t.Parallel()
	_, get := newERPFixture(t)
	_, body := get("GET", "/contacts")
	if !strings.Contains(body, `sm:hidden"><span class="tabular-nums">081200000001</span></span>`) {
		t.Error("phone meta line should start with the phone when the contact has no route or class")
	}
	if strings.Contains(body, `sm:hidden"> · `) {
		t.Error("phone meta line starts with a separator")
	}
}
