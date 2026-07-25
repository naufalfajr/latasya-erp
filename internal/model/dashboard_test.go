package model_test

import (
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestDashboardMonthlyTrends_AccrualAndCashIntegrity(t *testing.T) {
	db := testutil.SetupTestDB(t)
	accountID := func(code string) int {
		t.Helper()
		var id int
		if err := db.QueryRow(`SELECT id FROM accounts WHERE code = ?`, code).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	cash := accountID("1-1001")
	bank := accountID("1-1002")
	ar := accountID("1-1100")
	prepaid := accountID("1-1200")
	revenue := accountID("4-1001")
	expense := accountID("5-1001")

	post := func(date string, posted bool, lines ...model.JournalLine) {
		t.Helper()
		if _, err := model.CreateJournalEntry(db, &model.JournalEntry{
			EntryDate: date, Description: "trend fixture", IsPosted: posted, CreatedBy: 1,
		}, lines); err != nil {
			t.Fatal(err)
		}
	}

	// Opening cash before the selected 6-month window (Oct 2025 - Mar 2026).
	post("2025-08-15", true, model.JournalLine{AccountID: cash, Debit: 1000}, model.JournalLine{AccountID: revenue, Credit: 1000})
	// Revenue can accrue without changing cash; prepaid assets are not cash.
	post("2026-02-10", true, model.JournalLine{AccountID: ar, Debit: 500}, model.JournalLine{AccountID: revenue, Credit: 500})
	post("2026-02-12", true, model.JournalLine{AccountID: prepaid, Debit: 200}, model.JournalLine{AccountID: cash, Credit: 200})
	post("2026-02-20", true, model.JournalLine{AccountID: expense, Debit: 300}, model.JournalLine{AccountID: cash, Credit: 300})
	// Reversal preserves signed values.
	post("2026-03-04", true, model.JournalLine{AccountID: revenue, Debit: 100}, model.JournalLine{AccountID: ar, Credit: 100})
	// Cash-to-cash transfer cancels.
	post("2026-03-06", true, model.JournalLine{AccountID: bank, Debit: 400}, model.JournalLine{AccountID: cash, Credit: 400})
	// Unposted and after-as-of entries do not count.
	post("2026-03-08", false, model.JournalLine{AccountID: cash, Debit: 900}, model.JournalLine{AccountID: revenue, Credit: 900})
	post("2026-03-21", true, model.JournalLine{AccountID: cash, Debit: 800}, model.JournalLine{AccountID: revenue, Credit: 800})
	if _, err := db.Exec(`UPDATE accounts SET is_active = 0 WHERE id = ?`, cash); err != nil {
		t.Fatal(err)
	}

	asOf := time.Date(2026, time.March, 20, 18, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
	got, err := model.GetDashboardDataAt(db, "monthly", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CashConfigured || got.CashBalance == nil || *got.CashBalance != 500 {
		t.Fatalf("cash balance: configured=%v value=%v, want 500", got.CashConfigured, got.CashBalance)
	}
	if len(got.Trends) != 6 {
		t.Fatalf("trends: got %d want 6", len(got.Trends))
	}
	feb := got.Trends[len(got.Trends)-2]
	if feb.Label != "Feb 2026" {
		t.Errorf("February label: got %q", feb.Label)
	}
	if feb.Revenue != 500 || feb.Expenses != 300 || feb.NetIncome != 200 {
		t.Errorf("February profitability: %+v", feb)
	}
	if feb.NetCashMovement == nil || *feb.NetCashMovement != -500 || feb.ClosingCash == nil || *feb.ClosingCash != 500 {
		t.Errorf("February cash: %+v", feb)
	}
	mar := got.Trends[len(got.Trends)-1]
	if !mar.IsPartial || mar.EndDate != "2026-03-20" {
		t.Errorf("March MTD boundary: %+v", mar)
	}
	if mar.Revenue != -100 || mar.Expenses != 0 || mar.NetIncome != -100 {
		t.Errorf("March reversal profitability: %+v", mar)
	}
	if mar.NetCashMovement == nil || *mar.NetCashMovement != 0 || mar.ClosingCash == nil || *mar.ClosingCash != 500 {
		t.Errorf("March transfer should net to zero and carry cash: %+v", mar)
	}
}

func TestDashboardMonthlyTrends_UsesJakartaMonthBoundary(t *testing.T) {
	db := testutil.SetupTestDB(t)
	// 18:30 UTC on July 31 is already August 1 in Jakarta.
	got, err := model.GetDashboardDataAt(db, "monthly", time.Date(2026, time.July, 31, 18, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.AsOf != "2026-08-01" || got.Trends[5].Label != "Aug 2026" || !got.Trends[5].IsPartial {
		t.Fatalf("Jakarta boundary not applied: as_of=%s trend=%+v", got.AsOf, got.Trends[5])
	}
}

func TestDashboardMonthlyTrends_NoCashClassification(t *testing.T) {
	db := testutil.SetupTestDB(t)
	if _, err := db.Exec(`UPDATE accounts SET is_cash = 0`); err != nil {
		t.Fatal(err)
	}
	got, err := model.GetDashboardDataAt(db, "monthly", time.Date(2026, time.June, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.CashConfigured || got.CashBalance != nil {
		t.Fatalf("cash should be unavailable: %+v", got)
	}
	for _, trend := range got.Trends {
		if trend.ClosingCash != nil || trend.NetCashMovement != nil {
			t.Fatalf("cash trend should be unavailable: %+v", trend)
		}
	}
}

func TestDashboardQuarterlyTrends_AlignToCalendarQuarters(t *testing.T) {
	db := testutil.SetupTestDB(t)
	accountID := func(code string) int {
		t.Helper()
		var id int
		if err := db.QueryRow(`SELECT id FROM accounts WHERE code = ?`, code).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	cash := accountID("1-1001")
	revenue := accountID("4-1001")
	expense := accountID("5-1001")
	post := func(date string, debitAccount, creditAccount, amount int) {
		t.Helper()
		if _, err := model.CreateJournalEntry(db, &model.JournalEntry{
			EntryDate: date, Description: "quarterly fixture", IsPosted: true, CreatedBy: 1,
		}, []model.JournalLine{{AccountID: debitAccount, Debit: amount}, {AccountID: creditAccount, Credit: amount}}); err != nil {
			t.Fatal(err)
		}
	}
	// Jan (complete) and Feb (elapsed-to-date) both fall in Q1 2026; Mar has not started.
	post("2026-01-10", cash, revenue, 100)
	post("2026-02-05", cash, revenue, 40)
	post("2026-02-06", expense, cash, 10)

	// Asia/Jakarta is UTC+7, so 10:00 UTC on Feb 15 is 17:00 local — still Feb 15 local.
	asOf := time.Date(2026, time.February, 15, 10, 0, 0, 0, time.UTC)
	got, err := model.GetDashboardDataAt(db, "quarterly", asOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trends) != 6 {
		t.Fatalf("trends: got %d want 6", len(got.Trends))
	}
	q1 := got.Trends[len(got.Trends)-1]
	if q1.Label != "Q1 2026" {
		t.Fatalf("current quarter label: got %q want Q1 2026", q1.Label)
	}
	if q1.StartDate != "2026-01-01" || q1.EndDate != "2026-02-15" || !q1.IsPartial {
		t.Fatalf("current quarter bounds: %+v", q1)
	}
	if q1.Revenue != 140 || q1.Expenses != 10 || q1.NetIncome != 130 {
		t.Fatalf("current quarter totals (Jan complete + Feb-to-date): %+v", q1)
	}
	prevQ := got.Trends[len(got.Trends)-2]
	if prevQ.Label != "Q4 2025" || prevQ.IsPartial {
		t.Fatalf("preceding quarter should be the complete Q4 2025: %+v", prevQ)
	}
}

// TestGetDashboardData covers the thin GetDashboardData wrapper, which just
// calls GetDashboardDataAt with "monthly" granularity and BusinessNow().
func TestGetDashboardData(t *testing.T) {
	db := testutil.SetupTestDB(t)

	got, err := model.GetDashboardData(db)
	if err != nil {
		t.Fatal(err)
	}
	if got.Granularity != "monthly" {
		t.Errorf("granularity: got %q, want %q", got.Granularity, "monthly")
	}
	if len(got.Trends) != 6 {
		t.Errorf("trends: got %d want 6", len(got.Trends))
	}
}
