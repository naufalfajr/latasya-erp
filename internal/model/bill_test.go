package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestCreateBill(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SPBU Pertamina', 'supplier', 1)")
	var contactID, fuelID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SPBU Pertamina'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	b := &model.Bill{
		ContactID: contactID, BillDate: "2026-04-04", DueDate: "2026-04-30",
		Notes: "Diesel fuel", CreatedBy: 1,
	}
	lines := []model.BillLine{
		{Description: "Solar 200L", Quantity: 100, UnitPrice: 2000000, AccountID: fuelID},
	}

	billID, err := model.CreateBill(db, b, lines)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	created, _ := model.GetBill(db, billID)
	if created.Status != "draft" {
		t.Errorf("expected 'draft', got %q", created.Status)
	}
	if created.Total != 2000000 {
		t.Errorf("expected total 2000000, got %d", created.Total)
	}
	if created.BillNumber == "" {
		t.Error("expected auto-generated bill number")
	}
}

func TestReceiveBill(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Supplier', 'supplier', 1)")
	var contactID, fuelID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Supplier'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	billID, _ := model.CreateBill(db, &model.Bill{
		ContactID: contactID, BillDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.BillLine{
		{Description: "Fuel", Quantity: 100, UnitPrice: 2000000, AccountID: fuelID},
	})

	err := model.ReceiveBill(db, billID, 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	received, _ := model.GetBill(db, billID)
	if received.Status != "received" {
		t.Errorf("expected 'received', got %q", received.Status)
	}
	if received.JournalID == nil {
		t.Error("expected journal_id set")
	}

	// Verify journal entry
	je, _ := model.GetJournalEntry(db, *received.JournalID)
	if je.TotalDebit != 2000000 || je.TotalCredit != 2000000 {
		t.Errorf("expected balanced 2000000, got debit=%d credit=%d", je.TotalDebit, je.TotalCredit)
	}
}

func TestBillPaymentLifecycle(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Supplier', 'supplier', 1)")
	var contactID, fuelID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Supplier'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	billID, _ := model.CreateBill(db, &model.Bill{
		ContactID: contactID, BillDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.BillLine{
		{Description: "Fuel", Quantity: 100, UnitPrice: 2000000, AccountID: fuelID},
	})
	model.ReceiveBill(db, billID, 1)

	// Partial payment
	err := model.RecordBillPayment(db, billID, 1000000, "2026-04-10", cashID, 1)
	if err != nil {
		t.Fatalf("partial payment: %v", err)
	}
	partial, _ := model.GetBill(db, billID)
	if partial.Status != "partial" {
		t.Errorf("expected 'partial', got %q", partial.Status)
	}

	// Full payment
	err = model.RecordBillPayment(db, billID, 1000000, "2026-04-15", cashID, 1)
	if err != nil {
		t.Fatalf("full payment: %v", err)
	}
	paid, _ := model.GetBill(db, billID)
	if paid.Status != "paid" {
		t.Errorf("expected 'paid', got %q", paid.Status)
	}
}

func TestDeleteBill_OnlyDraft(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Supplier', 'supplier', 1)")
	var contactID, fuelID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Supplier'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '5-1001'").Scan(&fuelID)

	billID, _ := model.CreateBill(db, &model.Bill{
		ContactID: contactID, BillDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.BillLine{
		{Description: "Test", Quantity: 100, UnitPrice: 1000000, AccountID: fuelID},
	})

	// Delete draft — ok
	if err := model.DeleteBill(db, billID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Create and receive
	billID2, _ := model.CreateBill(db, &model.Bill{
		ContactID: contactID, BillDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.BillLine{
		{Description: "Test", Quantity: 100, UnitPrice: 1000000, AccountID: fuelID},
	})
	model.ReceiveBill(db, billID2, 1)

	// Delete received — should fail
	err := model.DeleteBill(db, billID2)
	if err == nil {
		t.Fatal("expected error deleting received bill")
	}
}
