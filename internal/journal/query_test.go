package journal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestListFiltersExactAccountAlongsideDateAndSearch(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := journal.New(db)
	userID := testutil.CreateTestUser(t, db, "journal-filter", "secret", "admin")
	actor := journal.Actor{UserID: userID, CanManageJournals: true}
	assetID := accountID(t, db, model.AccountTypeAsset)
	revenueID := accountID(t, db, model.AccountTypeRevenue)
	liabilityID := accountID(t, db, model.AccountTypeLiability)
	var childAssetID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE account_type='asset' AND id<>? ORDER BY code LIMIT 1", assetID).Scan(&childAssetID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE accounts SET parent_id=? WHERE id=?", assetID, childAssetID); err != nil {
		t.Fatal(err)
	}

	create := func(date, description string, lines ...journal.Line) {
		t.Helper()
		if _, err := module.CreateManual(context.Background(), actor, journal.ManualDraft{
			EntryDate: date, Description: description, Lines: lines,
		}); err != nil {
			t.Fatalf("create %q: %v", description, err)
		}
	}
	create("2026-04-10", "April tuition collection",
		journal.Line{AccountID: assetID, Debit: 500}, journal.Line{AccountID: revenueID, Credit: 500})
	create("2026-04-11", "April unrelated collection",
		journal.Line{AccountID: assetID, Debit: 250}, journal.Line{AccountID: liabilityID, Credit: 250})
	create("2026-05-10", "May tuition collection",
		journal.Line{AccountID: assetID, Debit: 300}, journal.Line{AccountID: revenueID, Credit: 300})
	create("2026-04-12", "April tuition via payable",
		journal.Line{AccountID: liabilityID, Debit: 150}, journal.Line{AccountID: revenueID, Credit: 150})
	create("2026-04-13", "Child asset only",
		journal.Line{AccountID: childAssetID, Debit: 75}, journal.Line{AccountID: revenueID, Credit: 75})

	result, err := module.List(context.Background(), journal.Filter{
		AccountID: assetID, DateFrom: "2026-04-01", DateTo: "2026-04-30", Search: "tuition",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Entries) != 1 {
		t.Fatalf("filtered entries total=%d rows=%d, want 1 and 1", result.Total, len(result.Entries))
	}
	if result.Entries[0].Description != "April tuition collection" {
		t.Fatalf("entry=%q, want the exact-account April tuition entry", result.Entries[0].Description)
	}

	// Selecting the parent does not include entries posted to a child account.
	parentOnly, err := module.List(context.Background(), journal.Filter{AccountID: assetID, Search: "Child asset only"})
	if err != nil {
		t.Fatal(err)
	}
	if parentOnly.Total != 0 || len(parentOnly.Entries) != 0 {
		t.Fatalf("exact account filter matched a different account: total=%d rows=%d", parentOnly.Total, len(parentOnly.Entries))
	}
	var assetName string
	if err := db.QueryRow("SELECT name FROM accounts WHERE id=?", assetID).Scan(&assetName); err != nil {
		t.Fatal(err)
	}
	byAccountName, err := module.List(context.Background(), journal.Filter{Search: assetName})
	if err != nil {
		t.Fatal(err)
	}
	if byAccountName.Total != 0 || len(byAccountName.Entries) != 0 {
		t.Fatalf("text search should remain limited to reference and description, got %d account-name matches", byAccountName.Total)
	}
}

func TestAccountFilterOptionsIncludeActiveAndOnlyHistoricalInactiveAccounts(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := journal.New(db)
	userID := testutil.CreateTestUser(t, db, "journal-options", "secret", "admin")
	actor := journal.Actor{UserID: userID, CanManageJournals: true}
	assetID := accountID(t, db, model.AccountTypeAsset)
	revenueID := accountID(t, db, model.AccountTypeRevenue)

	unusedInactive, err := account.New(db).Create(context.Background(), account.Actor{UserID: userID, CanManage: true}, account.Draft{
		Code: "9-9901", Name: "Unused old account", AccountType: model.AccountTypeAsset, NormalBalance: "debit",
	})
	if err != nil {
		t.Fatal(err)
	}
	unusedActive, err := account.New(db).Create(context.Background(), account.Actor{UserID: userID, CanManage: true}, account.Draft{
		Code: "9-9902", Name: "Unused active account", AccountType: model.AccountTypeAsset, NormalBalance: "debit", IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE accounts SET is_active=0 WHERE id IN (?, ?)", assetID, unusedInactive.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := module.CreateManual(context.Background(), actor, journal.ManualDraft{
		EntryDate: "2026-04-01", Description: "Historical entry",
		Lines: []journal.Line{{AccountID: assetID, Debit: 100}, {AccountID: revenueID, Credit: 100}},
	}); err != nil {
		t.Fatal(err)
	}

	options, err := module.AccountFilterOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]model.Account{}
	for _, option := range options {
		seen[option.ID] = option
	}
	if _, ok := seen[revenueID]; !ok {
		t.Fatal("active account should be available")
	}
	if _, ok := seen[unusedActive.ID]; !ok {
		t.Fatal("active account without journal history should still be available")
	}
	if old, ok := seen[assetID]; !ok || old.IsActive {
		t.Fatal("inactive account with journal history should be available and remain inactive")
	}
	if _, ok := seen[unusedInactive.ID]; ok {
		t.Fatal("inactive account without journal history should not be available")
	}
	typeOrder := map[string]int{"asset": 1, "liability": 2, "equity": 3, "revenue": 4, "expense": 5}
	for i := 1; i < len(options); i++ {
		previous, current := options[i-1], options[i]
		if typeOrder[previous.AccountType] > typeOrder[current.AccountType] ||
			(previous.AccountType == current.AccountType && previous.Code > current.Code) {
			t.Fatalf("options are not sorted by type and code: %q then %q", previous.Code, current.Code)
		}
	}
}

func TestAccountSummariesUseFirstJournalLineAndCountRemainingLines(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := journal.New(db)
	userID := testutil.CreateTestUser(t, db, "journal-summary", "secret", "admin")
	actor := journal.Actor{UserID: userID, CanManageJournals: true}
	assetID := accountID(t, db, model.AccountTypeAsset)
	revenueID := accountID(t, db, model.AccountTypeRevenue)
	liabilityID := accountID(t, db, model.AccountTypeLiability)

	entry, err := module.CreateManual(context.Background(), actor, journal.ManualDraft{
		EntryDate: "2026-04-01", Description: "Three lines",
		Lines: []journal.Line{
			{AccountID: liabilityID, Debit: 100},
			{AccountID: assetID, Debit: 50},
			{AccountID: revenueID, Credit: 150},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := []int{entry.ID}
	summaries, err := module.AccountSummaries(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := summaries[entry.ID]
	if !ok {
		t.Fatal("summary missing for paginated entry")
	}
	var firstCode, firstName string
	if err := db.QueryRow("SELECT code, name FROM accounts WHERE id=?", liabilityID).Scan(&firstCode, &firstName); err != nil {
		t.Fatal(err)
	}
	if summary.AccountCode != firstCode || summary.AccountName != firstName || summary.LineCount != 3 {
		t.Fatalf("summary=%+v, want first line %q · %q and 3 lines", summary, firstCode, firstName)
	}
	if _, err := module.AccountSummaries(context.Background(), nil); err != nil {
		t.Fatalf("empty page summary lookup should be a no-op: %v", err)
	}
}

func TestAccountFilterQueryUsesCoveringCompositeIndex(t *testing.T) {
	db := testutil.SetupTestDB(t)
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT je.id FROM journal_entries je
		WHERE EXISTS (SELECT 1 FROM journal_lines jl WHERE jl.entry_id=je.id AND jl.account_id=?)`, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "idx_jl_account_entry") || !strings.Contains(joined, "COVERING INDEX") {
		t.Fatalf("account filter should use the covering account/entry index; query plan:\n%s", joined)
	}
}
