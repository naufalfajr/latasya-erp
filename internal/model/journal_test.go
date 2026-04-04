package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestCreateJournalEntry_Balanced(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Find account IDs for Cash and Revenue
	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate:   "2026-04-04",
		Description: "School bus payment from SD Negeri 1",
		SourceType:  "manual",
		IsPosted:    true,
		CreatedBy:   1,
	}

	lines := []model.JournalLine{
		{AccountID: cashID, Debit: 5000000, Credit: 0, Memo: "Cash received"},
		{AccountID: revenueID, Debit: 0, Credit: 5000000, Memo: "Monthly contract"},
	}

	entryID, err := model.CreateJournalEntry(db, je, lines)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if entryID == 0 {
		t.Fatal("expected non-zero entry ID")
	}

	// Verify the entry
	entry, err := model.GetJournalEntry(db, entryID)
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if entry.Description != "School bus payment from SD Negeri 1" {
		t.Errorf("expected description match, got %q", entry.Description)
	}
	if len(entry.Lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(entry.Lines))
	}
	if entry.TotalDebit != 5000000 {
		t.Errorf("expected total debit 5000000, got %d", entry.TotalDebit)
	}
	if entry.TotalCredit != 5000000 {
		t.Errorf("expected total credit 5000000, got %d", entry.TotalCredit)
	}
}

func TestCreateJournalEntry_Unbalanced(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate:   "2026-04-04",
		Description: "Unbalanced entry",
		SourceType:  "manual",
		IsPosted:    true,
		CreatedBy:   1,
	}

	lines := []model.JournalLine{
		{AccountID: cashID, Debit: 5000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 3000000}, // Different amount!
	}

	_, err := model.CreateJournalEntry(db, je, lines)
	if err == nil {
		t.Fatal("expected error for unbalanced entry")
	}
}

func TestCreateJournalEntry_EmptyLines(t *testing.T) {
	db := testutil.SetupTestDB(t)

	je := &model.JournalEntry{
		EntryDate:   "2026-04-04",
		Description: "Empty entry",
		SourceType:  "manual",
		IsPosted:    true,
		CreatedBy:   1,
	}

	_, err := model.CreateJournalEntry(db, je, []model.JournalLine{})
	if err == nil {
		t.Fatal("expected error for empty lines")
	}
}

func TestCreateJournalEntry_GeneratesReference(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate:   "2026-04-04",
		Description: "Test reference",
		SourceType:  "manual",
		IsPosted:    true,
		CreatedBy:   1,
	}

	lines := []model.JournalLine{
		{AccountID: cashID, Debit: 1000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 1000000},
	}

	entryID, _ := model.CreateJournalEntry(db, je, lines)
	entry, _ := model.GetJournalEntry(db, entryID)

	if entry.Reference == "" {
		t.Error("expected auto-generated reference")
	}
	if len(entry.Reference) < 10 {
		t.Errorf("reference seems too short: %q", entry.Reference)
	}
}

func TestListJournalEntries(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	// Create two entries
	for i := 0; i < 2; i++ {
		je := &model.JournalEntry{
			EntryDate: "2026-04-04", Description: "Test entry",
			SourceType: "manual", IsPosted: true, CreatedBy: 1,
		}
		model.CreateJournalEntry(db, je, []model.JournalLine{
			{AccountID: cashID, Debit: 1000000, Credit: 0},
			{AccountID: revenueID, Debit: 0, Credit: 1000000},
		})
	}

	entries, err := model.ListJournalEntries(db, model.JournalFilter{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestListJournalEntries_FilterByDate(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	lines := []model.JournalLine{
		{AccountID: cashID, Debit: 1000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 1000000},
	}

	// Create entries on different dates
	je1 := &model.JournalEntry{EntryDate: "2026-03-15", Description: "March", SourceType: "manual", IsPosted: true, CreatedBy: 1}
	je2 := &model.JournalEntry{EntryDate: "2026-04-15", Description: "April", SourceType: "manual", IsPosted: true, CreatedBy: 1}
	model.CreateJournalEntry(db, je1, lines)
	model.CreateJournalEntry(db, je2, lines)

	// Filter April only
	entries, _ := model.ListJournalEntries(db, model.JournalFilter{DateFrom: "2026-04-01", DateTo: "2026-04-30"})
	if len(entries) != 1 {
		t.Errorf("expected 1 entry in April, got %d", len(entries))
	}
	if len(entries) > 0 && entries[0].Description != "April" {
		t.Errorf("expected 'April', got %q", entries[0].Description)
	}
}

func TestUpdateJournalEntry(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate: "2026-04-04", Description: "Original",
		SourceType: "manual", IsPosted: true, CreatedBy: 1,
	}
	entryID, _ := model.CreateJournalEntry(db, je, []model.JournalLine{
		{AccountID: cashID, Debit: 1000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 1000000},
	})

	// Update
	updated := &model.JournalEntry{ID: entryID, EntryDate: "2026-04-05", Description: "Updated"}
	newLines := []model.JournalLine{
		{AccountID: cashID, Debit: 2000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 2000000},
	}
	if err := model.UpdateJournalEntry(db, updated, newLines); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	entry, _ := model.GetJournalEntry(db, entryID)
	if entry.Description != "Updated" {
		t.Errorf("expected 'Updated', got %q", entry.Description)
	}
	if entry.TotalDebit != 2000000 {
		t.Errorf("expected total debit 2000000, got %d", entry.TotalDebit)
	}
}

func TestUpdateJournalEntry_Unbalanced(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate: "2026-04-04", Description: "Test",
		SourceType: "manual", IsPosted: true, CreatedBy: 1,
	}
	entryID, _ := model.CreateJournalEntry(db, je, []model.JournalLine{
		{AccountID: cashID, Debit: 1000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 1000000},
	})

	err := model.UpdateJournalEntry(db, &model.JournalEntry{ID: entryID, EntryDate: "2026-04-05", Description: "Bad"}, []model.JournalLine{
		{AccountID: cashID, Debit: 2000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 1000000}, // Unbalanced
	})
	if err == nil {
		t.Fatal("expected error for unbalanced update")
	}
}

func TestDeleteJournalEntry_Manual(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate: "2026-04-04", Description: "To delete",
		SourceType: "manual", IsPosted: true, CreatedBy: 1,
	}
	entryID, _ := model.CreateJournalEntry(db, je, []model.JournalLine{
		{AccountID: cashID, Debit: 1000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 1000000},
	})

	if err := model.DeleteJournalEntry(db, entryID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	_, err := model.GetJournalEntry(db, entryID)
	if err == nil {
		t.Error("expected error for deleted entry")
	}
}

func TestDeleteJournalEntry_AutoGenerated(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, revenueID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	je := &model.JournalEntry{
		EntryDate: "2026-04-04", Description: "Auto",
		SourceType: "income", IsPosted: true, CreatedBy: 1,
	}
	entryID, _ := model.CreateJournalEntry(db, je, []model.JournalLine{
		{AccountID: cashID, Debit: 1000000, Credit: 0},
		{AccountID: revenueID, Debit: 0, Credit: 1000000},
	})

	err := model.DeleteJournalEntry(db, entryID)
	if err == nil {
		t.Fatal("expected error for auto-generated entry deletion")
	}
}

func TestCreateJournalEntry_MultipleLines(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var cashID, fuelID, tollID int
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-5001'").Scan(&tollID)

	je := &model.JournalEntry{
		EntryDate: "2026-04-04", Description: "Trip expenses",
		SourceType: "manual", IsPosted: true, CreatedBy: 1,
	}

	// 3 lines: fuel + toll debits, cash credit
	lines := []model.JournalLine{
		{AccountID: fuelID, Debit: 500000, Credit: 0, Memo: "Diesel"},
		{AccountID: tollID, Debit: 150000, Credit: 0, Memo: "Toll Cipularang"},
		{AccountID: cashID, Debit: 0, Credit: 650000, Memo: "Cash payment"},
	}

	entryID, err := model.CreateJournalEntry(db, je, lines)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	entry, _ := model.GetJournalEntry(db, entryID)
	if len(entry.Lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(entry.Lines))
	}
	if entry.TotalDebit != 650000 || entry.TotalCredit != 650000 {
		t.Errorf("expected balanced 650000, got debit=%d credit=%d", entry.TotalDebit, entry.TotalCredit)
	}
}
