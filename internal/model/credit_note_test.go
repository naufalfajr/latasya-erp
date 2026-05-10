package model_test

import (
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestCreateCreditNote(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	cn := &model.CreditNote{
		ContactID: contactID,
		CNDate:    "2026-04-15",
		Reason:    model.CreditNoteReasonCancellation,
		Notes:     "Testing",
		CreatedBy: 1,
	}
	lines := []model.CreditNoteLine{
		{Description: "Refund", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	}

	cnID, err := model.CreateCreditNote(db, cn, lines)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if cnID == 0 {
		t.Fatal("expected non-zero id")
	}

	got, err := model.GetCreditNote(db, cnID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "draft" {
		t.Errorf("expected status draft, got %q", got.Status)
	}
	if got.Total != 1000000 {
		t.Errorf("expected total 1_000_000, got %d", got.Total)
	}
	if got.CNNumber == "" {
		t.Error("expected auto-generated number")
	}
	if len(got.Lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(got.Lines))
	}
}

func TestIssueCreditNote_FullCancellation(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	// Create + send a 5_000_000 invoice
	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	// Issue a credit note for the full amount
	cn := &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-10",
		Reason: model.CreditNoteReasonCancellation, CreatedBy: 1,
	}
	cnID, err := model.CreateCreditNote(db, cn, []model.CreditNoteLine{
		{Description: "Cancel service", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	})
	if err != nil {
		t.Fatalf("create cn: %v", err)
	}

	if err := model.IssueCreditNote(db, cnID, 1); err != nil {
		t.Fatalf("issue cn: %v", err)
	}

	issued, _ := model.GetCreditNote(db, cnID)
	if issued.Status != "issued" {
		t.Errorf("expected status issued, got %q", issued.Status)
	}
	if issued.JournalID == nil {
		t.Error("expected journal_id set")
	}

	// Reversing journal should be balanced
	je, err := model.GetJournalEntry(db, *issued.JournalID)
	if err != nil {
		t.Fatalf("get journal: %v", err)
	}
	if je.TotalDebit != 5000000 || je.TotalCredit != 5000000 {
		t.Errorf("expected balanced 5M, got debit=%d credit=%d", je.TotalDebit, je.TotalCredit)
	}

	// Invoice should now be cancelled (fully credited, never paid)
	inv, _ := model.GetInvoice(db, invID)
	if inv.Status != "cancelled" {
		t.Errorf("expected invoice cancelled, got %q", inv.Status)
	}
	if inv.AmountCredited != 5000000 {
		t.Errorf("expected amount_credited 5M, got %d", inv.AmountCredited)
	}
	if inv.AmountDue() != 0 {
		t.Errorf("expected amount due 0, got %d", inv.AmountDue())
	}
}

func TestIssueCreditNote_AfterPartialPayment(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	// Customer pays 2M, then we credit the remaining 3M
	if err := model.RecordInvoicePayment(db, invID, 2000000, "2026-04-10", cashID, 1); err != nil {
		t.Fatalf("payment: %v", err)
	}

	cn := &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonDiscount, CreatedBy: 1,
	}
	cnID, _ := model.CreateCreditNote(db, cn, []model.CreditNoteLine{
		{Description: "Goodwill discount", Quantity: 100, UnitPrice: 3000000, AccountID: revenueID},
	})
	if err := model.IssueCreditNote(db, cnID, 1); err != nil {
		t.Fatalf("issue: %v", err)
	}

	inv, _ := model.GetInvoice(db, invID)
	if inv.Status != "paid" {
		t.Errorf("expected paid (paid+credited covers total), got %q", inv.Status)
	}
	if inv.AmountDue() != 0 {
		t.Errorf("expected amount due 0, got %d", inv.AmountDue())
	}
}

func TestIssueCreditNote_OverCreditRejected(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)
	model.RecordInvoicePayment(db, invID, 600000, "2026-04-10", cashID, 1)

	// Try to credit more than what's outstanding (1M - 600k = 400k remaining)
	cn := &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonOther, CreatedBy: 1,
	}
	cnID, _ := model.CreateCreditNote(db, cn, []model.CreditNoteLine{
		{Description: "Too much", Quantity: 100, UnitPrice: 500000, AccountID: revenueID},
	})
	err := model.IssueCreditNote(db, cnID, 1)
	if err == nil {
		t.Fatal("expected error for over-credit")
	}
}

func TestVoidCreditNote(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	cn := &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonCancellation, CreatedBy: 1,
	}
	cnID, _ := model.CreateCreditNote(db, cn, []model.CreditNoteLine{
		{Description: "Cancel", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.IssueCreditNote(db, cnID, 1)

	// Now void it — invoice should return to 'sent' with no credit applied.
	if err := model.VoidCreditNote(db, cnID, 1); err != nil {
		t.Fatalf("void: %v", err)
	}

	voided, _ := model.GetCreditNote(db, cnID)
	if voided.Status != "void" {
		t.Errorf("expected status void, got %q", voided.Status)
	}

	inv, _ := model.GetInvoice(db, invID)
	if inv.Status != "sent" {
		t.Errorf("expected invoice back to sent, got %q", inv.Status)
	}
	if inv.AmountCredited != 0 {
		t.Errorf("expected amount_credited 0, got %d", inv.AmountCredited)
	}
}

func TestDeleteCreditNote_OnlyDraft(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	cn := &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonCancellation, CreatedBy: 1,
	}
	cnID, _ := model.CreateCreditNote(db, cn, []model.CreditNoteLine{
		{Description: "Cancel", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})

	// Delete draft — works
	if err := model.DeleteCreditNote(db, cnID); err != nil {
		t.Fatalf("delete draft: %v", err)
	}

	// Issue another, try to delete — should fail
	cnID2, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonCancellation, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Cancel", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.IssueCreditNote(db, cnID2, 1)

	if err := model.DeleteCreditNote(db, cnID2); err == nil {
		t.Fatal("expected error deleting issued credit note")
	}
}

func TestListCreditNotesForInvoice(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	for i := 0; i < 2; i++ {
		model.CreateCreditNote(db, &model.CreditNote{
			ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
			Reason: model.CreditNoteReasonOther, CreatedBy: 1,
		}, []model.CreditNoteLine{
			{Description: "Adj", Quantity: 100, UnitPrice: 100000, AccountID: revenueID},
		})
	}

	notes, err := model.ListCreditNotesForInvoice(db, invID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(notes) != 2 {
		t.Errorf("expected 2 credit notes, got %d", len(notes))
	}
}

func TestUpdateCreditNote(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	cnID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, CNDate: "2026-04-15",
		Reason: model.CreditNoteReasonOther, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Original", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})

	updated := &model.CreditNote{
		ID: cnID, ContactID: contactID, CNDate: "2026-04-16",
		Reason: model.CreditNoteReasonDiscount, Notes: "edited",
	}
	newLines := []model.CreditNoteLine{
		{Description: "Updated", Quantity: 100, UnitPrice: 2000000, AccountID: revenueID},
	}
	if err := model.UpdateCreditNote(db, updated, newLines); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := model.GetCreditNote(db, cnID)
	if got.Total != 2000000 {
		t.Errorf("expected total 2_000_000, got %d", got.Total)
	}
	if len(got.Lines) != 1 || got.Lines[0].Description != "Updated" {
		t.Errorf("expected single 'Updated' line, got %+v", got.Lines)
	}
}

func TestUpdateCreditNote_RejectsNonDraft(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	cnID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonCancellation, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Cancel", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.IssueCreditNote(db, cnID, 1)

	err := model.UpdateCreditNote(db, &model.CreditNote{
		ID: cnID, ContactID: contactID, CNDate: "2026-04-13", Reason: model.CreditNoteReasonOther,
	}, []model.CreditNoteLine{
		{Description: "Try", Quantity: 100, UnitPrice: 500000, AccountID: revenueID},
	})
	if err == nil {
		t.Fatal("expected error updating issued credit note")
	}
}

func TestIssueCreditNote_RejectsDraftInvoice(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})

	cnID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonCancellation, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Refund", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	err := model.IssueCreditNote(db, cnID, 1)
	if err == nil {
		t.Fatal("expected error for draft invoice")
	}
	if !strings.Contains(err.Error(), "draft") {
		t.Errorf("expected error to mention 'draft', got %q", err.Error())
	}
}

func TestIssueCreditNote_RejectsCancelledInvoice(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	cn1ID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonCancellation, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Cancel", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	if err := model.IssueCreditNote(db, cn1ID, 1); err != nil {
		t.Fatalf("first issue: %v", err)
	}

	cn2ID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-13",
		Reason: model.CreditNoteReasonOther, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Extra", Quantity: 100, UnitPrice: 100000, AccountID: revenueID},
	})
	err := model.IssueCreditNote(db, cn2ID, 1)
	if err == nil {
		t.Fatal("expected error for cancelled invoice")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("expected error to mention 'cancelled', got %q", err.Error())
	}
}

func TestIssueCreditNote_RejectsPaidInvoice(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID, cashID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '1-1001'").Scan(&cashID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)
	model.RecordInvoicePayment(db, invID, 1000000, "2026-04-10", cashID, 1)

	cnID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonOther, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Refund", Quantity: 100, UnitPrice: 100000, AccountID: revenueID},
	})
	err := model.IssueCreditNote(db, cnID, 1)
	if err == nil {
		t.Fatal("expected error issuing CN against paid invoice (no remaining balance)")
	}
}

func TestIssueCreditNote_WithTax(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30",
		TaxAmount: 500000, CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	cnID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonCancellation, TaxAmount: 500000, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Refund", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	})
	if err := model.IssueCreditNote(db, cnID, 1); err != nil {
		t.Fatalf("issue: %v", err)
	}

	issued, _ := model.GetCreditNote(db, cnID)
	je, err := model.GetJournalEntry(db, *issued.JournalID)
	if err != nil {
		t.Fatalf("get journal: %v", err)
	}
	if len(je.Lines) != 3 {
		t.Errorf("expected 3 journal lines, got %d", len(je.Lines))
	}
	if je.TotalDebit != 5500000 || je.TotalCredit != 5500000 {
		t.Errorf("expected balanced 5.5M, got debit=%d credit=%d", je.TotalDebit, je.TotalCredit)
	}
}

func TestIssueCreditNote_TaxExceedsInvoiceTax(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30",
		TaxAmount: 100000, CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	cnID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonOther, TaxAmount: 200000, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Refund", Quantity: 100, UnitPrice: 10000, AccountID: revenueID},
	})
	err := model.IssueCreditNote(db, cnID, 1)
	if err == nil {
		t.Fatal("expected tax-exceeds error")
	}
	if !strings.Contains(err.Error(), "tax") {
		t.Errorf("expected error to mention 'tax', got %q", err.Error())
	}
}

func TestIssueCreditNote_MultiplePartialCNs(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 3000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	for i := 0; i < 2; i++ {
		cnID, _ := model.CreateCreditNote(db, &model.CreditNote{
			ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
			Reason: model.CreditNoteReasonDiscount, CreatedBy: 1,
		}, []model.CreditNoteLine{
			{Description: "Partial", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
		})
		if err := model.IssueCreditNote(db, cnID, 1); err != nil {
			t.Fatalf("issue %d: %v", i, err)
		}
	}

	inv, _ := model.GetInvoice(db, invID)
	if inv.AmountCredited != 2000000 {
		t.Errorf("expected amount_credited 2M, got %d", inv.AmountCredited)
	}
	if inv.Status == "cancelled" {
		t.Errorf("expected invoice not yet cancelled after 2 of 3 credited, got %q", inv.Status)
	}

	cn3ID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-13",
		Reason: model.CreditNoteReasonDiscount, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Final", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	if err := model.IssueCreditNote(db, cn3ID, 1); err != nil {
		t.Fatalf("issue final: %v", err)
	}

	inv2, _ := model.GetInvoice(db, invID)
	if inv2.Status != "cancelled" {
		t.Errorf("expected invoice cancelled after full credit, got %q", inv2.Status)
	}
	if inv2.AmountCredited != 3000000 {
		t.Errorf("expected amount_credited 3M, got %d", inv2.AmountCredited)
	}
}

func TestIssueCreditNote_ContactMismatch(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust A', 'customer', 1)")
	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust B', 'customer', 1)")
	var custA, custB, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust A'").Scan(&custA)
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust B'").Scan(&custB)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: custA, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	cnID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: custB, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonOther, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Refund", Quantity: 100, UnitPrice: 500000, AccountID: revenueID},
	})
	err := model.IssueCreditNote(db, cnID, 1)
	if err == nil {
		t.Fatal("expected contact mismatch error")
	}
	if !strings.Contains(err.Error(), "contact") {
		t.Errorf("expected error to mention 'contact', got %q", err.Error())
	}
}

func TestListCreditNotes_FilterByStatus(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Cust', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Cust'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	invID, _ := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "Service", Quantity: 100, UnitPrice: 5000000, AccountID: revenueID},
	})
	model.SendInvoice(db, invID, 1)

	for i := 0; i < 2; i++ {
		model.CreateCreditNote(db, &model.CreditNote{
			ContactID: contactID, CNDate: "2026-04-12",
			Reason: model.CreditNoteReasonOther, CreatedBy: 1,
		}, []model.CreditNoteLine{
			{Description: "Draft", Quantity: 100, UnitPrice: 100000, AccountID: revenueID},
		})
	}
	issuedID, _ := model.CreateCreditNote(db, &model.CreditNote{
		ContactID: contactID, InvoiceID: &invID, CNDate: "2026-04-12",
		Reason: model.CreditNoteReasonCancellation, CreatedBy: 1,
	}, []model.CreditNoteLine{
		{Description: "Issued", Quantity: 100, UnitPrice: 500000, AccountID: revenueID},
	})
	if err := model.IssueCreditNote(db, issuedID, 1); err != nil {
		t.Fatalf("issue: %v", err)
	}

	drafts, _ := model.ListCreditNotes(db, model.CreditNoteFilter{Status: "draft"})
	if len(drafts) != 2 {
		t.Errorf("expected 2 drafts, got %d", len(drafts))
	}
	issued, _ := model.ListCreditNotes(db, model.CreditNoteFilter{Status: "issued"})
	if len(issued) != 1 {
		t.Errorf("expected 1 issued, got %d", len(issued))
	}
}
