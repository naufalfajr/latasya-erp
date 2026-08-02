package reporting_test

import (
	"context"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/reporting"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestTrialBalance_Balanced(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	var cashID, revenueID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	// Record income: debit cash 10M, credit revenue 10M
	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 10000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 10000000},
	})

	// Record expense: debit fuel 3M, credit cash 3M
	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-05", Description: "Fuel", SourceType: "expense", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: fuelID, Debit: 3000000, Credit: 0},
		{AccountID: cashID, Debit: 0, Credit: 3000000},
	})

	rows, err := reporting.New(db).TrialBalance(context.Background(), "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var totalDebit, totalCredit int
	for _, r := range rows {
		totalDebit += r.TotalDebit
		totalCredit += r.TotalCredit
	}

	if totalDebit != totalCredit {
		t.Errorf("trial balance not balanced: debit=%d credit=%d", totalDebit, totalCredit)
	}
	if totalDebit != 13000000 {
		t.Errorf("expected total debit 13000000, got %d", totalDebit)
	}
}

func TestTrialBalance_Empty(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	rows, err := reporting.New(db).TrialBalance(context.Background(), "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty period, got %d", len(rows))
	}
}

func TestProfitLoss(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	var cashID, revenueID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	// Income 10M
	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 10000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 10000000},
	})

	// Expense 3M
	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-05", Description: "Fuel", SourceType: "expense", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: fuelID, Debit: 3000000, Credit: 0},
		{AccountID: cashID, Debit: 0, Credit: 3000000},
	})

	report, err := reporting.New(db).ProfitLoss(context.Background(), "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if report.TotalRevenue != 10000000 {
		t.Errorf("expected revenue 10000000, got %d", report.TotalRevenue)
	}
	if report.TotalExpense != 3000000 {
		t.Errorf("expected expense 3000000, got %d", report.TotalExpense)
	}
	if report.NetIncome != 7000000 {
		t.Errorf("expected net income 7000000, got %d", report.NetIncome)
	}
}

func TestBalanceSheet_Equation(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	var cashID, revenueID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	// Income 10M
	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 10000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 10000000},
	})

	// Expense 3M
	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-05", Description: "Fuel", SourceType: "expense", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: fuelID, Debit: 3000000, Credit: 0},
		{AccountID: cashID, Debit: 0, Credit: 3000000},
	})

	report, err := reporting.New(db).BalanceSheet(context.Background(), "2026-04-30")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Assets = Liabilities + Equity (including retained earnings)
	if report.Assets.Total != report.TotalLiabEquity {
		t.Errorf("balance sheet doesn't balance: assets=%d, L+E=%d",
			report.Assets.Total, report.TotalLiabEquity)
	}

	// Cash should be 7M (10M income - 3M expense)
	if report.Assets.Total != 7000000 {
		t.Errorf("expected assets 7000000, got %d", report.Assets.Total)
	}

	// Retained earnings should be 7M (net income)
	if report.RetainedEarnings != 7000000 {
		t.Errorf("expected retained earnings 7000000, got %d", report.RetainedEarnings)
	}
}

func TestGeneralLedger(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	// Two transactions touching cash
	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income 1", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 5000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 5000000},
	})

	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-10", Description: "Income 2", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 3000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 3000000},
	})

	entries, err := reporting.New(db).GeneralLedger(context.Background(), cashID, "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Running balance: 5M, then 8M
	if entries[0].Balance != 5000000 {
		t.Errorf("expected first balance 5000000, got %d", entries[0].Balance)
	}
	if entries[1].Balance != 8000000 {
		t.Errorf("expected second balance 8000000, got %d", entries[1].Balance)
	}
}

func TestCashFlow(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	var cashID, revenueID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 10000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 10000000},
	})

	testutil.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-05", Description: "Fuel", SourceType: "expense", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: fuelID, Debit: 3000000, Credit: 0},
		{AccountID: cashID, Debit: 0, Credit: 3000000},
	})

	report, err := reporting.New(db).CashFlow(context.Background(), "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Closing cash should be 7M
	if report.ClosingCash != 7000000 {
		t.Errorf("expected closing cash 7000000, got %d", report.ClosingCash)
	}
}

func TestCashFlow_UsesClassification(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	var cash, bank, ar, revenue int
	for code, target := range map[string]*int{
		"1-1001": &cash, "1-1002": &bank, "1-1100": &ar, "4-1001": &revenue,
	} {
		if err := db.QueryRow(`SELECT id FROM accounts WHERE code = ?`, code).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	post := func(date string, lines ...model.JournalLine) {
		t.Helper()
		if _, err := testutil.CreateJournalEntry(db, &model.JournalEntry{
			EntryDate: date, Description: "cash fixture", IsPosted: true, CreatedBy: 1,
		}, lines); err != nil {
			t.Fatal(err)
		}
	}
	post("2026-03-01", model.JournalLine{AccountID: cash, Debit: 1000}, model.JournalLine{AccountID: revenue, Credit: 1000})
	post("2026-03-02", model.JournalLine{AccountID: ar, Debit: 500}, model.JournalLine{AccountID: revenue, Credit: 500})
	post("2026-03-03", model.JournalLine{AccountID: bank, Debit: 300}, model.JournalLine{AccountID: cash, Credit: 300})

	report, err := reporting.New(db).CashFlow(context.Background(), "2026-03-01", "2026-03-31")
	if err != nil {
		t.Fatal(err)
	}
	if !report.CashConfigured || report.TotalMovement != 1000 || report.NetCashChange != 1000 || report.ClosingCash != 1000 {
		t.Fatalf("cash report totals: %+v", report)
	}
}
