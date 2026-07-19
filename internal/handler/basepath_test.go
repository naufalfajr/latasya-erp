package handler_test

import (
	"database/sql"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
)

// TestAdminFormActions_RespectBasePath guards against the exact bug class
// found during manual review: a template building its POST target as
// {{if $data.IsEdit}}/contacts/{{.ID}}{{else}}/contacts{{end}} skips the
// leading {{$.BasePath}} because the literal "/contacts" text never sits
// right after action=" — a blind sed sweep for action="/ doesn't catch it,
// and the resulting form silently 404s once mounted under /dashboard.
func TestAdminFormActions_RespectBasePath(t *testing.T) {
	ts, db := testServer(t, "/dashboard")
	cookies := loginAsAdmin(t, ts, "/dashboard")

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('BP Contact', 'customer', 1)")
	var contactID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'BP Contact'").Scan(&contactID)
	invID := mustInvoice(t, db, contactID)

	cases := []struct {
		name       string
		path       string
		wantAction string
	}{
		{"contact new", "/dashboard/contacts/new", `action="/dashboard/contacts"`},
		{"contact edit", "/dashboard/contacts/" + strconv.Itoa(contactID) + "/edit", `action="/dashboard/contacts/` + strconv.Itoa(contactID) + `"`},
		{"invoice new", "/dashboard/invoices/new", `action="/dashboard/invoices"`},
		{"invoice edit", "/dashboard/invoices/" + strconv.Itoa(invID) + "/edit", `action="/dashboard/invoices/` + strconv.Itoa(invID) + `"`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := requestWithCookies(db, "GET", ts.URL+c.path, cookies, "")
			resp, err := (&http.Client{}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			text := string(body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d, body: %s", resp.StatusCode, text)
			}
			if !strings.Contains(text, c.wantAction) {
				t.Errorf("expected form action to contain %q, page did not; page may 404 under /dashboard in production", c.wantAction)
			}
		})
	}
}

// TestDashboard_RecentTransactionRow_RespectsBasePath guards the second bug
// found: an onclick="window.location='/journals/{{.ID}}'" row handler, which
// a href/action/hx-* sweep can't catch since it's plain JS in an attribute
// value, not a routed template attribute.
func TestDashboard_RecentTransactionRow_RespectsBasePath(t *testing.T) {
	ts, db := testServer(t, "/dashboard")
	cookies := loginAsAdmin(t, ts, "/dashboard")

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('BP Dash', 'customer', 1)")
	var contactID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'BP Dash'").Scan(&contactID)
	invID := mustInvoice(t, db, contactID)
	if err := model.SendInvoice(db, invID, 1); err != nil {
		t.Fatalf("send invoice: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/dashboard/", cookies, "")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	// html/template escapes the first "/" in a JS-string attribute context
	// (e.g. "\/dashboard/journals/1") as an XSS/breakout mitigation — that's
	// expected and browsers unescape it fine, so match loosely on the path.
	if !strings.Contains(text, "window.location=") || !strings.Contains(text, "dashboard/journals/") {
		t.Error("expected recent-transaction row navigation to include /dashboard prefix")
	}
}

// mustInvoice creates a minimal draft invoice for contactID and returns its
// ID. Shared by tests that only need an invoice to exist, not its contents.
func mustInvoice(t *testing.T, db *sql.DB, contactID int) int {
	t.Helper()
	var revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	invID, err := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-06-01", DueDate: "2026-06-11", CreatedBy: 1,
	}, []model.InvoiceLine{{Description: "x", Quantity: 100, UnitPrice: 100000, AccountID: revenueID}})
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	return invID
}
