package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestListAccounts_All(t *testing.T) {
	db := testutil.SetupTestDB(t)

	accounts, err := model.ListAccounts(db, model.AccountFilter{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(accounts) == 0 {
		t.Fatal("expected seeded accounts, got 0")
	}
	// Should have at least the seeded accounts (45)
	if len(accounts) < 40 {
		t.Errorf("expected at least 40 seeded accounts, got %d", len(accounts))
	}
}

func TestListAccounts_FilterByType(t *testing.T) {
	db := testutil.SetupTestDB(t)

	types := []string{"asset", "liability", "equity", "revenue", "expense"}
	for _, typ := range types {
		accounts, err := model.ListAccounts(db, model.AccountFilter{Type: typ})
		if err != nil {
			t.Fatalf("filter by %s: %v", typ, err)
		}
		if len(accounts) == 0 {
			t.Errorf("expected accounts for type %s, got 0", typ)
		}
		for _, a := range accounts {
			if a.AccountType != typ {
				t.Errorf("expected type %s, got %s for account %s", typ, a.AccountType, a.Code)
			}
		}
	}
}

func TestListAccounts_FilterByActive(t *testing.T) {
	db := testutil.SetupTestDB(t)

	active := true
	accounts, err := model.ListAccounts(db, model.AccountFilter{IsActive: &active})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for _, a := range accounts {
		if !a.IsActive {
			t.Errorf("expected active account, got inactive: %s", a.Code)
		}
	}
}

func TestListAccounts_Search(t *testing.T) {
	db := testutil.SetupTestDB(t)

	accounts, err := model.ListAccounts(db, model.AccountFilter{Search: "fuel"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(accounts) == 0 {
		t.Fatal("expected to find fuel accounts")
	}
}

func TestCreateAccount(t *testing.T) {
	db := testutil.SetupTestDB(t)

	a := &model.Account{
		Code:          "9-0001",
		Name:          "Test Account",
		AccountType:   "asset",
		NormalBalance: "debit",
		IsActive:      true,
		Description:   "A test account",
	}

	if err := model.CreateAccount(db, a); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify it was created
	accounts, _ := model.ListAccounts(db, model.AccountFilter{Search: "Test Account"})
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].Code != "9-0001" {
		t.Errorf("expected code '9-0001', got %q", accounts[0].Code)
	}
}

func TestCreateAccount_DuplicateCode(t *testing.T) {
	db := testutil.SetupTestDB(t)

	a := &model.Account{
		Code:          "1-1001", // Already exists (Cash on Hand)
		Name:          "Duplicate",
		AccountType:   "asset",
		NormalBalance: "debit",
		IsActive:      true,
	}

	err := model.CreateAccount(db, a)
	if err == nil {
		t.Fatal("expected error for duplicate code")
	}
}

func TestGetAccount(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Get first account
	accounts, _ := model.ListAccounts(db, model.AccountFilter{})
	if len(accounts) == 0 {
		t.Fatal("no accounts found")
	}

	account, err := model.GetAccount(db, accounts[0].ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if account.Code != accounts[0].Code {
		t.Errorf("expected code %q, got %q", accounts[0].Code, account.Code)
	}
}

func TestUpdateAccount(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Create a test account
	a := &model.Account{
		Code: "9-0002", Name: "Original", AccountType: "asset",
		NormalBalance: "debit", IsActive: true,
	}
	model.CreateAccount(db, a)

	accounts, _ := model.ListAccounts(db, model.AccountFilter{Search: "Original"})
	if len(accounts) == 0 {
		t.Fatal("test account not found")
	}

	// Update it
	accounts[0].Name = "Updated"
	if err := model.UpdateAccount(db, &accounts[0]); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	updated, _ := model.GetAccount(db, accounts[0].ID)
	if updated.Name != "Updated" {
		t.Errorf("expected name 'Updated', got %q", updated.Name)
	}
}

func TestDeleteAccount_NonSystem(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Create a non-system account
	a := &model.Account{
		Code: "9-0003", Name: "To Delete", AccountType: "asset",
		NormalBalance: "debit", IsActive: true,
	}
	model.CreateAccount(db, a)

	accounts, _ := model.ListAccounts(db, model.AccountFilter{Search: "To Delete"})
	if len(accounts) == 0 {
		t.Fatal("test account not found")
	}

	if err := model.DeleteAccount(db, accounts[0].ID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify deleted
	_, err := model.GetAccount(db, accounts[0].ID)
	if err == nil {
		t.Error("expected error for deleted account")
	}
}

func TestDeleteAccount_SystemProtected(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Find a system account (Cash on Hand)
	accounts, _ := model.ListAccounts(db, model.AccountFilter{Search: "Cash on Hand"})
	if len(accounts) == 0 {
		t.Fatal("Cash on Hand not found")
	}

	// Try to delete — should not actually delete (WHERE is_system = 0)
	model.DeleteAccount(db, accounts[0].ID)

	// Should still exist
	_, err := model.GetAccount(db, accounts[0].ID)
	if err != nil {
		t.Error("system account should not be deleted")
	}
}

func TestListAccounts_OrderedByCode(t *testing.T) {
	db := testutil.SetupTestDB(t)

	accounts, err := model.ListAccounts(db, model.AccountFilter{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	for i := 1; i < len(accounts); i++ {
		if accounts[i].Code < accounts[i-1].Code {
			t.Errorf("accounts not ordered by code: %s before %s", accounts[i-1].Code, accounts[i].Code)
			break
		}
	}
}
