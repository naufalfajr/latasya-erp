package model_test

import (
	"database/sql"
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

func TestGenerateRecurringInvoices(t *testing.T) {
	db := testutil.SetupTestDB(t)
	seedManualSchoolClosure(t, db, "Sunday only", "2026-06-07", "2026-06-07")

	// Active customer with distance pricing — should get a new invoice.
	db.Exec("INSERT INTO contacts (name, contact_type, is_active, distance_km, has_sibling_discount, is_return_only) VALUES ('Active With Price', 'customer', 1, 8.5, 1, 1)")
	var withPriceID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Active With Price'").Scan(&withPriceID)

	// Active customer at 0 km still uses the 0–3 km base tier.
	db.Exec("INSERT INTO contacts (name, contact_type, is_active, distance_km) VALUES ('Active Short Distance', 'customer', 1, 0)")

	// Inactive customer with distance — excluded from the batch entirely.
	db.Exec("INSERT INTO contacts (name, contact_type, is_active, distance_km) VALUES ('Inactive With Price', 'customer', 0, 12)")

	result, err := model.GenerateRecurringInvoices(db, "2026-06-03", "2026-06-13", 1)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Created != 2 {
		t.Errorf("created: got %d want 2", result.Created)
	}
	if result.Skipped != 0 {
		t.Errorf("skipped: got %d want 0", result.Skipped)
	}
	if result.EffectiveDays != 26 {
		t.Errorf("effective days: got %d want 26", result.EffectiveDays)
	}
	if result.MultiplierPercent != 100 {
		t.Errorf("multiplier percent: got %d want 100", result.MultiplierPercent)
	}

	// The generated invoice uses the contact price formula, is dated this month, draft.
	var newTotal int
	var newStatus, newDue string
	if err := db.QueryRow(
		"SELECT total, status, due_date FROM invoices WHERE contact_id = ? AND substr(invoice_date,1,7) = '2026-06'",
		withPriceID,
	).Scan(&newTotal, &newStatus, &newDue); err != nil {
		t.Fatalf("fetch generated invoice: %v", err)
	}
	if newTotal != 350000 {
		t.Errorf("generated total: got %d want 350000", newTotal)
	}
	if newStatus != "draft" {
		t.Errorf("generated status: got %q want draft", newStatus)
	}
	if newDue != "2026-06-13" {
		t.Errorf("generated due date: got %q want 2026-06-13", newDue)
	}

	// Re-running in the same month must skip the already-invoiced customer.
	result2, err := model.GenerateRecurringInvoices(db, "2026-06-03", "2026-06-13", 1)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if result2.Created != 0 {
		t.Errorf("second run created: got %d want 0", result2.Created)
	}
}

func TestGenerateRecurringInvoicesAllowsPreviousMonthAndSkipsCurrentMonth(t *testing.T) {
	db := testutil.SetupTestDB(t)
	contactID := seedRecurringCustomer(t, db, "Monthly Customer", 5)

	var revenueID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID); err != nil {
		t.Fatalf("get revenue account: %v", err)
	}
	if _, err := model.CreateInvoice(db, &model.Invoice{
		ContactID: contactID, InvoiceDate: "2026-06-03", DueDate: "2026-06-13", CreatedBy: 1,
	}, []model.InvoiceLine{
		{Description: "June service", Quantity: 100, UnitPrice: 400000, AccountID: revenueID},
	}); err != nil {
		t.Fatalf("create June draft: %v", err)
	}

	firstJulyRun, err := model.GenerateRecurringInvoices(db, "2026-07-03", "2026-07-13", 1)
	if err != nil {
		t.Fatalf("first July generate: %v", err)
	}
	if firstJulyRun.Created != 1 || firstJulyRun.Skipped != 0 {
		t.Fatalf("first July result: created=%d skipped=%d, want created=1 skipped=0", firstJulyRun.Created, firstJulyRun.Skipped)
	}

	secondJulyRun, err := model.GenerateRecurringInvoices(db, "2026-07-03", "2026-07-13", 1)
	if err != nil {
		t.Fatalf("second July generate: %v", err)
	}
	if secondJulyRun.Created != 0 || secondJulyRun.Skipped != 1 {
		t.Fatalf("second July result: created=%d skipped=%d, want created=0 skipped=1", secondJulyRun.Created, secondJulyRun.Skipped)
	}

	var julyCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM invoices WHERE contact_id = ? AND substr(invoice_date, 1, 7) = '2026-07'",
		contactID,
	).Scan(&julyCount); err != nil {
		t.Fatalf("count July invoices: %v", err)
	}
	if julyCount != 1 {
		t.Fatalf("July invoice count = %d, want 1", julyCount)
	}
}

func TestGenerateRecurringInvoicesApplies85PercentMultiplier(t *testing.T) {
	db := testutil.SetupTestDB(t)
	seedManualSchoolClosure(t, db, "June break", "2026-06-01", "2026-06-08")
	contactID := seedRecurringCustomer(t, db, "Medium Closure Customer", 5)

	result, err := model.GenerateRecurringInvoices(db, "2026-06-03", "2026-06-13", 1)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.EffectiveDays != 19 {
		t.Errorf("effective days: got %d want 19", result.EffectiveDays)
	}
	if result.MultiplierPercent != 85 {
		t.Errorf("multiplier percent: got %d want 85", result.MultiplierPercent)
	}
	assertGeneratedRecurringPrice(t, db, contactID, "2026-06", 340000)
}

func TestGenerateRecurringInvoicesApplies75PercentMultiplier(t *testing.T) {
	db := testutil.SetupTestDB(t)
	seedManualSchoolClosure(t, db, "Long June break", "2026-06-01", "2026-06-15")
	contactID := seedRecurringCustomer(t, db, "Low Closure Customer", 5)

	result, err := model.GenerateRecurringInvoices(db, "2026-06-03", "2026-06-13", 1)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.EffectiveDays != 13 {
		t.Errorf("effective days: got %d want 13", result.EffectiveDays)
	}
	if result.MultiplierPercent != 75 {
		t.Errorf("multiplier percent: got %d want 75", result.MultiplierPercent)
	}
	assertGeneratedRecurringPrice(t, db, contactID, "2026-06", 300000)
}

func seedManualSchoolClosure(t *testing.T, db *sql.DB, title, startDate, endDate string) {
	t.Helper()
	_, err := model.CreateSchoolClosure(db, &model.SchoolClosure{
		Source:    model.SchoolClosureSourceManual,
		Title:     title,
		StartDate: startDate,
		EndDate:   endDate,
	})
	if err != nil {
		t.Fatalf("seed school closure %q: %v", title, err)
	}
}

func seedRecurringCustomer(t *testing.T, db *sql.DB, name string, distanceKm float64) int {
	t.Helper()
	result, err := db.Exec(
		"INSERT INTO contacts (name, contact_type, is_active, distance_km) VALUES (?, 'customer', 1, ?)",
		name, distanceKm,
	)
	if err != nil {
		t.Fatalf("seed recurring customer: %v", err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

func assertGeneratedRecurringPrice(t *testing.T, db *sql.DB, contactID int, month string, want int) {
	t.Helper()
	var total, unitPrice int
	if err := db.QueryRow(
		`SELECT i.total, il.unit_price
		 FROM invoices i JOIN invoice_lines il ON il.invoice_id = i.id
		 WHERE i.contact_id = ? AND substr(i.invoice_date, 1, 7) = ?`,
		contactID, month,
	).Scan(&total, &unitPrice); err != nil {
		t.Fatalf("fetch generated recurring invoice: %v", err)
	}
	if unitPrice != want {
		t.Errorf("generated unit price: got %d want %d", unitPrice, want)
	}
	if total != want {
		t.Errorf("generated total: got %d want %d", total, want)
	}
}

func TestBulkDeleteDraftInvoices(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Bulk Co', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Bulk Co'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	mk := func() int {
		id, _ := model.CreateInvoice(db, &model.Invoice{
			ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
		}, []model.InvoiceLine{{Description: "x", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID}})
		return id
	}

	d1 := mk()
	d2 := mk()
	sent := mk()
	model.SendInvoice(db, sent, 1)

	deleted, skipped, err := model.BulkDeleteDraftInvoices(db, []int{d1, d2, sent, 999999})
	if err != nil {
		t.Fatalf("bulk delete: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("deleted: got %d want 2", len(deleted))
	}
	if len(skipped) != 2 {
		t.Errorf("skipped: got %v want 2 entries (the sent + the missing id)", skipped)
	}

	remaining, _ := model.ListInvoices(db, model.InvoiceFilter{})
	if len(remaining) != 1 {
		t.Errorf("remaining invoices: got %d want 1", len(remaining))
	}
}

func TestInvoiceNumberNoCollisionAfterDelete(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Seq Co', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Seq Co'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	mk := func() (int, string) {
		inv := &model.Invoice{ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1}
		id, err := model.CreateInvoice(db, inv, []model.InvoiceLine{
			{Description: "x", Quantity: 100, UnitPrice: 1000, AccountID: revenueID},
		})
		if err != nil {
			t.Fatalf("create invoice: %v", err)
		}
		return id, inv.InvoiceNumber
	}

	_, n1 := mk()
	id2, _ := mk()
	_, n3 := mk()

	if _, _, err := model.BulkDeleteDraftInvoices(db, []int{id2}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The next number must not collide with the surviving n1/n3.
	id4, n4 := mk()
	if id4 == 0 {
		t.Fatal("expected a new invoice to be created")
	}
	if n4 == n1 || n4 == n3 {
		t.Fatalf("invoice-number collision after mid-sequence delete: n4=%q (n1=%q n3=%q)", n4, n1, n3)
	}
}

func TestBulkSendInvoices(t *testing.T) {
	db := testutil.SetupTestDB(t)

	db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES ('Send Co', 'customer', 1)")
	var contactID, revenueID int
	db.QueryRow("SELECT id FROM contacts WHERE name = 'Send Co'").Scan(&contactID)
	db.QueryRow("SELECT id FROM accounts WHERE code = '4-1001'").Scan(&revenueID)

	mk := func() int {
		id, _ := model.CreateInvoice(db, &model.Invoice{
			ContactID: contactID, InvoiceDate: "2026-04-04", DueDate: "2026-04-30", CreatedBy: 1,
		}, []model.InvoiceLine{{Description: "x", Quantity: 100, UnitPrice: 1000000, AccountID: revenueID}})
		return id
	}

	d1 := mk()
	d2 := mk()
	alreadySent := mk()
	model.SendInvoice(db, alreadySent, 1)

	res, err := model.BulkSendInvoices(db, []int{d1, d2, alreadySent, 999999}, 1)
	if err != nil {
		t.Fatalf("bulk send: %v", err)
	}
	if len(res.Sent) != 2 {
		t.Errorf("sent: got %d want 2", len(res.Sent))
	}
	if len(res.Skipped) != 2 {
		t.Errorf("skipped: got %v want 2 (already-sent + missing id)", res.Skipped)
	}
	if len(res.Failed) != 0 {
		t.Errorf("failed: got %v want 0", res.Failed)
	}
	for _, s := range res.Sent {
		if s.JournalID == nil {
			t.Errorf("sent invoice %d: expected journal_id in result", s.ID)
		}
	}

	for _, id := range []int{d1, d2} {
		inv, _ := model.GetInvoice(db, id)
		if inv.Status != "sent" {
			t.Errorf("invoice %d status: got %q want sent", id, inv.Status)
		}
		if inv.JournalID == nil {
			t.Errorf("invoice %d: expected a journal entry after send", id)
		}
	}
}
