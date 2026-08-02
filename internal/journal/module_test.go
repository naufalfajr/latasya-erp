package journal_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestCreateAllocatesUniqueReferencesAcrossModuleInstances(t *testing.T) {
	prime := testutil.SetupTestDB(t)
	prime.Close()
	dbPath := filepath.Join(t.TempDir(), "journals.db")
	db1, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db1.Close() })
	if err := database.Seed(db1); err != nil {
		t.Fatal(err)
	}
	db2, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db2.Close() })
	asset := accountID(t, db1, model.AccountTypeAsset)
	revenue := accountID(t, db1, model.AccountTypeRevenue)
	draft := journal.ManualDraft{EntryDate: "2026-08-01", Description: "Concurrent",
		Lines: []journal.Line{{AccountID: asset, Debit: 100}, {AccountID: revenue, Credit: 100}}}

	modules := []*journal.Module{journal.New(db1), journal.New(db2)}
	references := make(chan string, len(modules))
	errs := make(chan error, len(modules))
	var wg sync.WaitGroup
	for _, module := range modules {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry, err := module.CreateManual(context.Background(), journal.Actor{UserID: 1, CanManageJournals: true}, draft)
			if err != nil {
				errs <- err
				return
			}
			references <- entry.Reference
		}()
	}
	wg.Wait()
	close(errs)
	close(references)
	for err := range errs {
		t.Errorf("concurrent CreateManual: %v", err)
	}
	unique := map[string]bool{}
	for reference := range references {
		if unique[reference] {
			t.Errorf("duplicate reference %q", reference)
		}
		unique[reference] = true
	}
	if len(unique) != len(modules) {
		t.Fatalf("created %d unique references, want %d", len(unique), len(modules))
	}

	seed, err := modules[0].CreateManual(context.Background(), journal.Actor{UserID: 1, CanManageJournals: true}, journal.ManualDraft{
		EntryDate: "2026-08-01", Description: "Before concurrent updates",
		Lines: []journal.Line{{AccountID: asset, Debit: 100}, {AccountID: revenue, Credit: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	errs = make(chan error, len(modules))
	for i, module := range modules {
		wg.Add(1)
		amount := (i + 2) * 100
		go func() {
			defer wg.Done()
			_, err := module.UpdateManual(context.Background(), journal.Actor{UserID: 1, CanManageJournals: true}, seed.ID,
				journal.ManualDraft{EntryDate: "2026-08-02", Description: fmt.Sprintf("Update %d", amount),
					Lines: []journal.Line{{AccountID: asset, Debit: amount}, {AccountID: revenue, Credit: amount}}})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent UpdateManual: %v", err)
	}
	rows, err := db1.Query(`SELECT metadata FROM audit_log WHERE action='journal.update' AND target_id=? ORDER BY id`, seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	beforeTotals, afterTotals := map[int]bool{}, map[int]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			Before map[string]any `json:"before"`
			After  map[string]any `json:"after"`
		}
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			t.Fatal(err)
		}
		beforeTotals[int(metadata.Before["total"].(float64))] = true
		afterTotals[int(metadata.After["total"].(float64))] = true
	}
	if len(beforeTotals) != 2 || !beforeTotals[100] || len(afterTotals) != 2 || !afterTotals[200] || !afterTotals[300] {
		t.Fatalf("audit snapshots were not serialized: before=%v after=%v", beforeTotals, afterTotals)
	}
}

func TestManualLifecycleAndSourceBoundary(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "journal-owner", "secret", "admin")
	module := journal.New(db)
	actor := journal.Actor{UserID: userID, CanManageJournals: true}
	asset := accountID(t, db, model.AccountTypeAsset)
	revenue := accountID(t, db, model.AccountTypeRevenue)

	created, err := module.CreateManual(context.Background(), actor, journal.ManualDraft{
		EntryDate: "2026-08-01", Description: "Opening adjustment",
		Lines: []journal.Line{{AccountID: asset, Debit: 125000}, {AccountID: revenue, Credit: 125000}},
	})
	if err != nil {
		t.Fatalf("create manual journal: %v", err)
	}
	if created.SourceType != model.SourceManual || created.Reference == "" || created.TotalDebit != 125000 {
		t.Fatalf("unexpected created journal: %#v", created)
	}
	if _, err := time.Parse(time.RFC3339, created.CreatedAt); err != nil {
		t.Fatalf("created_at %q is not RFC3339: %v", created.CreatedAt, err)
	}

	updated, err := module.UpdateManual(context.Background(), actor, created.ID, journal.ManualDraft{
		EntryDate: "2026-08-02", Description: "Corrected adjustment",
		Lines: []journal.Line{{AccountID: asset, Debit: 150000}, {AccountID: revenue, Credit: 150000}},
	})
	if err != nil {
		t.Fatalf("update manual journal: %v", err)
	}
	if updated.Description != "Corrected adjustment" || updated.TotalCredit != 150000 {
		t.Fatalf("unexpected updated journal: %#v", updated)
	}

	if _, err := module.DeleteIncome(context.Background(), journal.Actor{UserID: userID, CanManageIncome: true}, created.ID); err == nil {
		t.Fatal("income delete should not cross the manual source boundary")
	}
	if _, err := module.DeleteManual(context.Background(), actor, created.ID); err != nil {
		t.Fatalf("delete manual journal: %v", err)
	}
	if _, err := module.Get(context.Background(), created.ID); !errors.Is(err, journal.ErrNotFound) {
		t.Fatalf("get deleted journal error = %v, want ErrNotFound", err)
	}
}

func TestIncomeAndExpenseEnforceShapesAndCapabilities(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "accounting-owner", "secret", "admin")
	module := journal.New(db)
	asset := accountID(t, db, model.AccountTypeAsset)
	revenue := accountID(t, db, model.AccountTypeRevenue)
	expense := accountID(t, db, model.AccountTypeExpense)

	if _, err := module.CreateIncome(context.Background(), journal.Actor{UserID: userID}, journal.IncomeDraft{
		EntryDate: "2026-08-01", Description: "Tuition", Amount: 500000,
		RevenueAccount: revenue, DepositAccount: asset,
	}); !errors.Is(err, journal.ErrForbidden) {
		t.Fatalf("create without capability error = %v, want ErrForbidden", err)
	}

	income, err := module.CreateIncome(context.Background(), journal.Actor{UserID: userID, CanManageIncome: true}, journal.IncomeDraft{
		EntryDate: "2026-08-01", Description: "Tuition", Amount: 500000,
		RevenueAccount: revenue, DepositAccount: asset,
	})
	if err != nil {
		t.Fatalf("create income: %v", err)
	}
	if income.SourceType != model.SourceIncome || income.Lines[0].Debit != 500000 || income.Lines[1].Credit != 500000 {
		t.Fatalf("unexpected income shape: %#v", income)
	}

	if _, err := module.CreateIncome(context.Background(), journal.Actor{UserID: userID, CanManageIncome: true}, journal.IncomeDraft{
		EntryDate: "2026-08-01", Description: "Wrong account", Amount: 1,
		RevenueAccount: expense, DepositAccount: asset,
	}); err == nil {
		t.Fatal("income accepted an expense account as revenue")
	}
	if _, err := module.CreateIncome(context.Background(), journal.Actor{UserID: userID, CanManageIncome: true}, journal.IncomeDraft{
		EntryDate: "2026-08-01", Description: "Same account", Amount: 1,
		RevenueAccount: asset, DepositAccount: asset,
	}); err == nil {
		t.Fatal("income accepted the same account on both sides")
	}

	expenseEntry, err := module.CreateExpense(context.Background(), journal.Actor{UserID: userID, CanManageExpenses: true}, journal.ExpenseDraft{
		EntryDate: "2026-08-02", Description: "Fuel", Amount: 90000,
		ExpenseAccount: expense, PaymentAccount: asset,
	})
	if err != nil {
		t.Fatalf("create expense: %v", err)
	}
	if expenseEntry.SourceType != model.SourceExpense || expenseEntry.Lines[0].Debit != 90000 || expenseEntry.Lines[1].Credit != 90000 {
		t.Fatalf("unexpected expense shape: %#v", expenseEntry)
	}

	result, err := module.List(context.Background(), journal.Filter{SourceType: model.SourceIncome})
	if err != nil {
		t.Fatalf("list income: %v", err)
	}
	if result.Total != 1 || len(result.Entries) != 1 || result.Entries[0].ID != income.ID {
		t.Fatalf("unexpected income list: %#v", result)
	}
}

func TestManualValidationDoesNotWritePartialEntry(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "journal-validator", "secret", "admin")
	module := journal.New(db)
	asset := accountID(t, db, model.AccountTypeAsset)

	_, err := module.CreateManual(context.Background(), journal.Actor{UserID: userID, CanManageJournals: true}, journal.ManualDraft{
		EntryDate: "2026-08-01", Description: "Unbalanced",
		Lines: []journal.Line{{AccountID: asset, Debit: 100}, {AccountID: asset, Credit: 99}},
	})
	var validation *journal.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("create error = %v, want ValidationError", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE description='Unbalanced'").Scan(&count); err != nil {
		t.Fatalf("count partial entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("partial journal entries = %d, want 0", count)
	}
}

func accountID(t *testing.T, db *sql.DB, accountType string) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM accounts WHERE account_type=? AND is_active=1 ORDER BY id LIMIT 1", accountType).Scan(&id); err != nil {
		t.Fatalf("find %s account: %v", accountType, err)
	}
	return id
}
