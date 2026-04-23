package handler_test

import (
	"net/http"
	"strings"
	"testing"
)

// Minimal smoke tests: render end-to-end, assert 200 + one expected keyword
// per page. Math correctness is covered by internal/model/report_test.go;
// these only guard the handler/template/routing seam from regressions.

func TestReport_TrialBalance_Renders(t *testing.T) {
	assertReportRenders(t, "/reports/trial-balance", "Trial Balance")
}

func TestReport_ProfitLoss_Renders(t *testing.T) {
	assertReportRenders(t, "/reports/profit-loss", "Profit")
}

func TestReport_BalanceSheet_Renders(t *testing.T) {
	assertReportRenders(t, "/reports/balance-sheet", "Balance Sheet")
}

func TestReport_CashFlow_Renders(t *testing.T) {
	assertReportRenders(t, "/reports/cash-flow", "Cash Flow")
}

func TestReport_GeneralLedger_Renders(t *testing.T) {
	assertReportRenders(t, "/reports/general-ledger", "General Ledger")
}

func TestReport_GeneralLedger_WithAccountFilter(t *testing.T) {
	ts, db := testServer(t)
	cookies := loginAsAdmin(t, ts)

	// Pick any real account so the handler exercises its "entries" branch.
	var accountID int
	if err := db.QueryRow("SELECT id FROM accounts LIMIT 1").Scan(&accountID); err != nil {
		t.Fatalf("no accounts seeded: %v", err)
	}

	client := &http.Client{}
	req, _ := requestWithCookies(db, "GET",
		ts.URL+"/reports/general-ledger?account="+itoa(accountID)+"&from=2026-01-01&to=2026-12-31",
		cookies, "")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
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
