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

	// Opening cash before the selected range.
	post("2026-01-15", true, model.JournalLine{AccountID: cash, Debit: 1000}, model.JournalLine{AccountID: revenue, Credit: 1000})
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
	got, err := model.GetDashboardDataAt(db, 3, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CashConfigured || got.CashBalance == nil || *got.CashBalance != 500 {
		t.Fatalf("cash balance: configured=%v value=%v, want 500", got.CashConfigured, got.CashBalance)
	}
	if len(got.MonthlyTrends) != 3 {
		t.Fatalf("trends: got %d want 3", len(got.MonthlyTrends))
	}
	feb := got.MonthlyTrends[1]
	if feb.Revenue != 500 || feb.Expenses != 300 || feb.NetIncome != 200 {
		t.Errorf("February profitability: %+v", feb)
	}
	if feb.NetCashMovement == nil || *feb.NetCashMovement != -500 || feb.ClosingCash == nil || *feb.ClosingCash != 500 {
		t.Errorf("February cash: %+v", feb)
	}
	mar := got.MonthlyTrends[2]
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
	got, err := model.GetDashboardDataAt(db, 6, time.Date(2026, time.July, 31, 18, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.AsOf != "2026-08-01" || got.MonthlyTrends[5].Month != "2026-08" || !got.MonthlyTrends[5].IsPartial {
		t.Fatalf("Jakarta boundary not applied: as_of=%s trend=%+v", got.AsOf, got.MonthlyTrends[5])
	}
}

func TestDashboardMonthlyTrends_NoCashClassification(t *testing.T) {
	db := testutil.SetupTestDB(t)
	if _, err := db.Exec(`UPDATE accounts SET is_cash = 0`); err != nil {
		t.Fatal(err)
	}
	got, err := model.GetDashboardDataAt(db, 6, time.Date(2026, time.June, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.CashConfigured || got.CashBalance != nil {
		t.Fatalf("cash should be unavailable: %+v", got)
	}
	for _, trend := range got.MonthlyTrends {
		if trend.ClosingCash != nil || trend.NetCashMovement != nil {
			t.Fatalf("cash trend should be unavailable: %+v", trend)
		}
	}
}
