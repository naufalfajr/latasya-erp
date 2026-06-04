package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestGetCompanyProfile_SeededDefaults(t *testing.T) {
	db := testutil.SetupTestDB(t)

	co, err := model.GetCompanyProfile(db)
	if err != nil {
		t.Fatalf("GetCompanyProfile: %v", err)
	}
	if co.Name != "Latasya Transport" {
		t.Errorf("Name = %q, want %q", co.Name, "Latasya Transport")
	}
	if co.Tagline != "School Bus & Travel Service" {
		t.Errorf("Tagline = %q, want %q", co.Tagline, "School Bus & Travel Service")
	}
}

func TestUpdateCompanyProfile_RoundTrip(t *testing.T) {
	db := testutil.SetupTestDB(t)

	want := &model.CompanyProfile{
		Name:              "PT Latasya Jaya",
		Tagline:           "Transport",
		Address:           "Jl. Mawar 1\nJakarta",
		Phone:             "021-555-0100",
		Email:             "billing@latasya.id",
		NPWP:              "01.234.567.8-901.000",
		BankName:          "BCA",
		BankAccountNumber: "1234567890",
		BankAccountHolder: "PT Latasya Jaya",
		InvoiceFooter:     "Terima kasih.",
	}
	if err := model.UpdateCompanyProfile(db, want); err != nil {
		t.Fatalf("UpdateCompanyProfile: %v", err)
	}

	got, err := model.GetCompanyProfile(db)
	if err != nil {
		t.Fatalf("GetCompanyProfile: %v", err)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.Address != want.Address {
		t.Errorf("Address = %q, want %q", got.Address, want.Address)
	}
	if got.NPWP != want.NPWP {
		t.Errorf("NPWP = %q, want %q", got.NPWP, want.NPWP)
	}
	if got.BankAccountNumber != want.BankAccountNumber {
		t.Errorf("BankAccountNumber = %q, want %q", got.BankAccountNumber, want.BankAccountNumber)
	}
}
