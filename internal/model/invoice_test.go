package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestCreateInvoice(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SD Negeri 1', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SD Negeri 1'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	inv := &model.Invoice{
		ContactID:   contactID,
		InvoiceDate: "2026-04-04",
		DueDate:     "2026-04-30",
		Notes:       "Monthly bus fee",
		CreatedBy:   1,
	}

	lines := []model.InvoiceLine{
		{Description: "School bus April 2026", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	}

	invID, err := model.CreateInvoice(db, inv, lines)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if invID == 0 {
		t.Fatal("expected non-zero invoice ID")
	}

	// Verify
	created, err := model.GetInvoice(db, invID)
	if err != nil {
		t.Fatalf("get invoice: %v", err)
	}
	if created.Status != "draft" {
		t.Errorf("expected status 'draft', got %q", created.Status)
	}
	if created.Total != 5000000 {
		t.Errorf("expected total 5000000, got %d", created.Total)
	}
	if created.InvoiceNumber == "" {
		t.Error("expected auto-generated invoice number")
	}
	if len(created.Lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(created.Lines))
	}
}

func TestSendInvoice(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('SD Negeri 1', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'SD Negeri 1'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	inv := &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}
	lines := []model.InvoiceLine{
		{Description: "Bus fee", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	}
	invID, _ := model.CreateInvoice(db, inv, lines)

	// Send it
	err := model.SendInvoice(db, invID, 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify status changed
	sent, _ := model.GetInvoice(db, invID)
	if sent.Status != "sent" {
		t.Errorf("expected status 'sent', got %q", sent.Status)
	}
	if sent.JournalID == nil {
		t.Error("expected journal_id to be set")
	}

	// Verify journal entry exists with correct amounts
	je, err := model.GetJournalEntry(db, *sent.JournalID)
	if err != nil {
		t.Fatalf("get journal: %v", err)
	}
	if je.TotalDebit != 5000000 || je.TotalCredit != 5000000 {
		t.Errorf("expected balanced 5000000, got debit=%d credit=%d", je.TotalDebit, je.TotalCredit)
	}
}

func TestSendInvoice_OnlyDraft(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Test', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	inv := &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}
	invID, _ := model.CreateInvoice(db, inv, []model.InvoiceLine{
		{Description: "Test", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})

	// Send once
	model.SendInvoice(db, invID, 1)

	// Try sending again — should fail
	err := model.SendInvoice(db, invID, 1)
	if err == nil {
		t.Fatal("expected error sending non-draft invoice")
	}
}

func TestRecordInvoicePayment(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Test', 'customer', 1)")
	var contactID, revenueID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	inv := &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}
	invID, _ := model.CreateInvoice(db, inv, []model.InvoiceLine{
		{Description: "Test", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	// Partial payment
	err := model.RecordInvoicePayment(db, invID, 3000000, "2026-04-10", cashID, 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	partial, _ := model.GetInvoice(db, invID)
	if partial.Status != "partial" {
		t.Errorf("expected status 'partial', got %q", partial.Status)
	}
	if partial.AmountPaid != 3000000 {
		t.Errorf("expected amount_paid 3000000, got %d", partial.AmountPaid)
	}

	// Full payment
	err = model.RecordInvoicePayment(db, invID, 2000000, "2026-04-15", cashID, 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	paid, _ := model.GetInvoice(db, invID)
	if paid.Status != "paid" {
		t.Errorf("expected status 'paid', got %q", paid.Status)
	}
	if paid.AmountPaid != 5000000 {
		t.Errorf("expected amount_paid 5000000, got %d", paid.AmountPaid)
	}
}

func TestRecordInvoicePayment_ExceedsBalance(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Test', 'customer', 1)")
	var contactID, revenueID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	inv := &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}
	invID, _ := model.CreateInvoice(db, inv, []model.InvoiceLine{
		{Description: "Test", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	err := model.RecordInvoicePayment(db, invID, 2000000, "2026-04-10", cashID, 1)
	if err == nil {
		t.Fatal("expected error for overpayment")
	}
}

func TestDeleteInvoice_OnlyDraft(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Test', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	inv := &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}
	invID, _ := model.CreateInvoice(db, inv, []model.InvoiceLine{
		{Description: "Test", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})

	// Delete draft — should work
	err := model.DeleteInvoice(db, invID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Create another and send it
	invID2, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Test", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID2, 1)

	// Delete sent — should fail
	err = model.DeleteInvoice(db, invID2)
	if err == nil {
		t.Fatal("expected error deleting sent invoice")
	}
}

func TestListInvoices(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Test', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Test'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	// Create 2 invoices
	for i := 0; i < 2; i++ {
		model.CreateInvoice(db, &model.Invoice{
			ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
		}, []model.InvoiceLine{
			{Description: "Test", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
		})
	}

	invoices, err := model.ListInvoices(db, model.InvoiceFilter{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(invoices) != 2 {
		t.Errorf("expected 2 invoices, got %d", len(invoices))
	}

	// Filter by status
	drafts, _ := model.ListInvoices(db, model.InvoiceFilter{Status: "draft"})
	if len(drafts) != 2 {
		t.Errorf("expected 2 drafts, got %d", len(drafts))
	}
}
