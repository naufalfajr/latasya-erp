package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestTrialBalance_Balanced(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	// Record income: debit cash 10M, credit revenue 10M
	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 10000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 10000000},
	})

	// Record expense: debit fuel 3M, credit cash 3M
	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-05", Description: "Fuel", SourceType: "expense", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: fuelID, Debit: 3000000, Credit: 0},
		{AccountID: cashID, Debit: 0, Credit: 3000000},
	})

	rows, err := model.TrialBalance(db, "2026-04-01", "2026-04-30")
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
	db := testutil.SetupTestDB(t)

	rows, err := model.TrialBalance(db, "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty period, got %d", len(rows))
	}
}

func TestProfitLoss(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	// Income 10M
	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 10000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 10000000},
	})

	// Expense 3M
	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-05", Description: "Fuel", SourceType: "expense", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: fuelID, Debit: 3000000, Credit: 0},
		{AccountID: cashID, Debit: 0, Credit: 3000000},
	})

	report, err := model.ProfitLoss(db, "2026-04-01", "2026-04-30")
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
	db := testutil.SetupTestDB(t)

	var cashID, revenueID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	// Income 10M
	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 10000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 10000000},
	})

	// Expense 3M
	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-05", Description: "Fuel", SourceType: "expense", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: fuelID, Debit: 3000000, Credit: 0},
		{AccountID: cashID, Debit: 0, Credit: 3000000},
	})

	report, err := model.BalanceSheet(db, "2026-04-30")
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
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	// Two transactions touching cash
	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income 1", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 5000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 5000000},
	})

	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-10", Description: "Income 2", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 3000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 3000000},
	})

	entries, err := model.GeneralLedger(db, cashID, "2026-04-01", "2026-04-30")
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
	db := testutil.SetupTestDB(t)

	var cashID, revenueID, fuelID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-01", Description: "Income", SourceType: "income", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: cashID, Debit: 10000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 10000000},
	})

	model.CreateJournalEntry(db, &model.JournalEntry{
		EntryDate: "2026-04-05", Description: "Fuel", SourceType: "expense", IsPosted: true, CreatedBy: 1,
	}, []model.JournalLine{
		{AccountID: fuelID, Debit: 3000000, Credit: 0},
		{AccountID: cashID, Debit: 0, Credit: 3000000},
	})

	report, err := model.CashFlow(db, "2026-04-01", "2026-04-30")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Closing cash should be 7M
	if report.ClosingCash != 7000000 {
		t.Errorf("expected closing cash 7000000, got %d", report.ClosingCash)
	}
}
