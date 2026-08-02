package company_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/company"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestModuleCompanyProfileInvariants(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := company.New(db)
	profile, err := m.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	profile.Name = "Updated Company"
	if _, err := m.Update(context.Background(), company.Actor{}, *profile); !errors.Is(err, company.ErrForbidden) {
		t.Fatalf("update without permission: got %v", err)
	}
	manager := company.Actor{UserID: 1, CanManage: true}
	bad := *profile
	bad.Name = ""
	var validation *company.ValidationError
	if _, err := m.Update(context.Background(), manager, bad); !errors.As(err, &validation) || validation.Fields["name"] == "" {
		t.Fatalf("empty name: got %v", err)
	}
	var assetID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE account_type=? AND is_active=1 LIMIT 1", model.AccountTypeAsset).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	bad = *profile
	bad.DefaultRevenueAccountID = assetID
	if _, err := m.Update(context.Background(), manager, bad); !errors.As(err, &validation) || validation.Fields["default_revenue_account_id"] == "" {
		t.Fatalf("non-revenue default: got %v", err)
	}
	var revenueID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE account_type=? AND is_active=1 LIMIT 1", model.AccountTypeRevenue).Scan(&revenueID); err != nil {
		t.Fatal(err)
	}
	profile.DefaultRevenueAccountID = revenueID
	profile.BankAccountNumber = "SECRET-123456789"
	updated, err := m.Update(context.Background(), manager, *profile)
	if err != nil || updated.Name != profile.Name || updated.DefaultRevenueAccountID != revenueID {
		t.Fatalf("valid update: profile=%v err=%v", updated, err)
	}
	var metadata string
	if err := db.QueryRow("SELECT COALESCE(metadata,'') FROM audit_log WHERE action='company_profile.update' ORDER BY id DESC LIMIT 1").Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(metadata, profile.BankAccountNumber) || strings.Contains(metadata, "bank_account_number") {
		t.Fatalf("company audit metadata leaks bank account number: %s", metadata)
	}
}

func TestModuleCompanyAccountLookupFailureIsInternal(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := company.New(db)
	profile, err := m.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE accounts RENAME TO accounts_unavailable"); err != nil {
		t.Fatal(err)
	}
	_, err = m.Update(context.Background(), company.Actor{UserID: 1, CanManage: true}, *profile)
	var validation *company.ValidationError
	if err == nil || errors.As(err, &validation) || !strings.Contains(err.Error(), "validate default revenue account") {
		t.Fatalf("operational lookup failure should remain internal, got %v", err)
	}
}
