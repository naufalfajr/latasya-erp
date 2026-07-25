package handler_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
)

// Minimal smoke tests: render end-to-end, assert 200 + one expected keyword
// per page. Math correctness is covered by internal/model/report_test.go;
// these only guard the handler/template/routing seam from regressions.

func TestReport_TrialBalance_Renders(t *testing.T) {
	t.Parallel()
	assertReportRenders(t, "/reports/trial-balance", "Trial Balance")
}

func TestReport_ProfitLoss_Renders(t *testing.T) {
	t.Parallel()
	assertReportRenders(t, "/reports/profit-loss", "Profit")
}

func TestReport_BalanceSheet_Renders(t *testing.T) {
	t.Parallel()
	assertReportRenders(t, "/reports/balance-sheet", "Balance Sheet")
}

func TestReport_CashFlow_Renders(t *testing.T) {
	t.Parallel()
	assertReportRenders(t, "/reports/cash-flow", "Cash Flow")
}

func TestReport_GeneralLedger_Renders(t *testing.T) {
	t.Parallel()
	assertReportRenders(t, "/reports/general-ledger", "General Ledger")
}

func TestReport_GeneralLedger_WithAccountFilter(t *testing.T) {
	t.Parallel()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Seed a posted manual entry so the handler exercises its "entries" branch
	// and the template renders the Source column + totals footer (not just the
	// empty state). Target a credit-normal account (revenue) so the test also
	// guards the footer's natural-sign net logic.
	var cashID, revenueID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID); err != nil {
		t.Fatalf("cash account not seeded: %v", err)
	}
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID); err != nil {
		t.Fatalf("revenue account not seeded: %v", err)
	}

	if _, err := model.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-01", Description: "GL test entry", IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{
			{AccountID: cashID, Debit: 100000},
			{AccountID: revenueID, Credit: 100000},
		},
	); err != nil {
		t.Fatalf("seed journal entry: %v", err)
	}

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET",
		ts.URL+"/reports/general-ledger?account="+itoa(revenueID)+"&from=2026-01-01&to=2026-12-31",
		cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	body := readBody(t, resp)
	// Source column renders a "manual" badge for the blank source_type, and the
	// totals footer renders for the non-empty entries branch.
	if !strings.Contains(body, "manual") {
		t.Errorf("expected Source column badge 'manual' in body")
	}
	if !strings.Contains(body, "Total") {
		t.Errorf("expected totals footer ('Total') in body")
	}
	// Natural-sign net: for this credit-normal account the footer net is
	// +100.000, so "-Rp 100.000" should appear exactly once — in the running
	// balance column (debit-credit), NOT also in the footer net. A regression
	// to debit-credit net would make it appear twice.
	if got := strings.Count(body, "-Rp 100.000"); got != 1 {
		t.Errorf("expected footer net in natural (positive) sign: '-Rp 100.000' count = %d, want 1", got)
	}
}

// TestReport_TrialBalance_WithData, TestReport_ProfitLoss_WithData and
// TestReport_CashFlow_WithData each seed one posted journal entry so the
// handler's non-empty-rows path renders (the smoke tests above only
// exercise the zero-rows path); TrialBalance's totals-accumulation loop in
// particular never runs otherwise.
func TestReport_TrialBalance_WithData(t *testing.T) {
	t.Parallel()
	assertReportWithData(t, "/reports/trial-balance", "", 250000, "250.000")
}

func TestReport_ProfitLoss_WithData(t *testing.T) {
	t.Parallel()
	assertReportWithData(t, "/reports/profit-loss", model.SourceIncome, 750000, "750.000")
}

func TestReport_CashFlow_WithData(t *testing.T) {
	t.Parallel()
	assertReportWithData(t, "/reports/cash-flow", model.SourceIncome, 300000, "300.000")
}

func assertReportWithData(t *testing.T, path, sourceType string, amount int, wantSubstr string) {
	t.Helper()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	var cashID, revenueID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID); err != nil {
		t.Fatalf("cash account not seeded: %v", err)
	}
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID); err != nil {
		t.Fatalf("revenue account not seeded: %v", err)
	}
	if _, err := model.CreateJournalEntry(db,
		&model.JournalEntry{EntryDate: "2026-04-01", Description: "report test entry", SourceType: sourceType, IsPosted: true, CreatedBy: 1},
		[]model.JournalLine{
			{AccountID: cashID, Debit: amount},
			{AccountID: revenueID, Credit: amount},
		},
	); err != nil {
		t.Fatalf("seed journal entry: %v", err)
	}

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+path+"?from=2026-01-01&to=2026-12-31", cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: expected 200, got %d", path, resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, wantSubstr) {
		t.Errorf("%s: expected %q in body", path, wantSubstr)
	}
}

func assertReportRenders(t *testing.T, path, keyword string) {
	t.Helper()
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET", ts.URL+path, cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("%s: expected 200, got %d", path, resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.Contains(body, keyword) {
		t.Errorf("%s: body should contain %q", path, keyword)
	}
}

func itoa(n int) string {
	// tiny inline helper to avoid pulling strconv into every test file that
	// only needs one int→string for a URL param.
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 10)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
