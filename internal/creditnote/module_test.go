package creditnote_test

import (
	"context"
	"errors"
	"testing"

	"github.com/naufal/latasya-erp/internal/creditnote"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestIssueAndVoidAreAtomicWithLinkedInvoice(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES ('Customer','customer',1)")
	var contact, revenue int
	db.QueryRow("SELECT id FROM contacts WHERE name='Customer'").Scan(&contact)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenue)
	invoiceID, err := testutil.CreateInvoice(db, &model.Invoice{ContactID: contact, InvoiceDate: "2026-08-01", DueDate: "2026-08-31", CreatedBy: 1}, []model.InvoiceLine{{Description: "Service", Quantity: 100, UnitPrice: 1_000_000, AccountID: revenue}})
	if err != nil {
		t.Fatal(err)
	}
	if err = testutil.SendInvoice(db, invoiceID, 1); err != nil {
		t.Fatal(err)
	}
	m := creditnote.New(db)
	actor := creditnote.Actor{UserID: 1, CanManage: true}
	created, err := m.Create(context.Background(), actor, creditnote.Draft{ContactID: contact, InvoiceID: &invoiceID, Date: "2026-08-02", Reason: model.CreditNoteReasonCancellation, Lines: []creditnote.Line{{Description: "Cancel", Quantity: 100, UnitPrice: 1_000_000, AccountID: revenue}}})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := m.Issue(context.Background(), actor, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Status != model.StatusIssued || issued.JournalID == nil {
		t.Fatalf("issued=%+v", issued)
	}
	invoice, err := testutil.GetInvoice(db, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Status != model.StatusCancelled || invoice.AmountCredited != 1_000_000 {
		t.Fatalf("invoice after issue=%+v", invoice)
	}
	voided, err := m.Void(context.Background(), actor, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if voided.Status != model.StatusVoid {
		t.Fatalf("void status=%s", voided.Status)
	}
	invoice, err = testutil.GetInvoice(db, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Status != model.StatusSent || invoice.AmountCredited != 0 {
		t.Fatalf("invoice after void=%+v", invoice)
	}
	var unbalanced int
	db.QueryRow(`SELECT COUNT(*) FROM journal_entries je WHERE je.source_type='credit_note' AND (SELECT COALESCE(SUM(debit-credit),0) FROM journal_lines WHERE entry_id=je.id)<>0`).Scan(&unbalanced)
	if unbalanced != 0 {
		t.Fatalf("unbalanced journals=%d", unbalanced)
	}
}

func TestAuthorizationAndIssuedStateGates(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES ('Gates','customer',1)")
	var contact, revenue int
	db.QueryRow("SELECT id FROM contacts WHERE name='Gates'").Scan(&contact)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenue)
	invID, err := testutil.CreateInvoice(db, &model.Invoice{ContactID: contact, InvoiceDate: "2026-08-01", DueDate: "2026-08-31", CreatedBy: 1}, []model.InvoiceLine{{Description: "Service", Quantity: 100, UnitPrice: 1_000_000, AccountID: revenue}})
	if err != nil {
		t.Fatal(err)
	}
	if err = testutil.SendInvoice(db, invID, 1); err != nil {
		t.Fatal(err)
	}
	m := creditnote.New(db)
	d := creditnote.Draft{ContactID: contact, InvoiceID: &invID, Date: "2026-08-02", Reason: model.CreditNoteReasonOther, Lines: []creditnote.Line{{Description: "Credit", Quantity: 100, UnitPrice: 100_000, AccountID: revenue}}}
	if _, err = m.Create(context.Background(), creditnote.Actor{UserID: 1}, d); !errors.Is(err, creditnote.ErrForbidden) {
		t.Fatalf("forbidden create: %v", err)
	}
	actor := creditnote.Actor{UserID: 1, CanManage: true}
	created, err := m.Create(context.Background(), actor, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Issue(context.Background(), actor, created.ID); err != nil {
		t.Fatal(err)
	}
	var conflict *creditnote.ConflictError
	if _, err = m.Issue(context.Background(), actor, created.ID); !errors.As(err, &conflict) {
		t.Fatalf("double issue: %v", err)
	}
	if _, err = m.Update(context.Background(), actor, created.ID, d); !errors.As(err, &conflict) {
		t.Fatalf("update issued: %v", err)
	}
	if _, err = m.Delete(context.Background(), actor, created.ID); !errors.As(err, &conflict) {
		t.Fatalf("delete issued: %v", err)
	}
	draftOnly, err := m.Create(context.Background(), actor, creditnote.Draft{ContactID: contact, Date: "2026-08-03", Reason: model.CreditNoteReasonOther, Lines: d.Lines})
	if err != nil {
		t.Fatal(err)
	}
	draftOnlyDraft := creditnote.Draft{ContactID: contact, Date: "2026-08-04", Reason: model.CreditNoteReasonDiscount, Lines: []creditnote.Line{{Description: "Updated", Quantity: 100, UnitPrice: 50_000, AccountID: revenue}}}
	if _, err = m.Update(context.Background(), actor, draftOnly.ID, draftOnlyDraft); err != nil {
		t.Fatalf("update draft: %v", err)
	}
	if _, err = m.Delete(context.Background(), actor, draftOnly.ID); err != nil {
		t.Fatalf("delete draft: %v", err)
	}
}

func TestTaxCeilingAndMultipleCreditLimit(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES ('Limits','customer',1)")
	var contact, revenue int
	db.QueryRow("SELECT id FROM contacts WHERE name='Limits'").Scan(&contact)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenue)
	invID, err := testutil.CreateInvoice(db, &model.Invoice{ContactID: contact, InvoiceDate: "2026-08-01", DueDate: "2026-08-31", TaxAmount: 100_000, CreatedBy: 1}, []model.InvoiceLine{{Description: "Service", Quantity: 100, UnitPrice: 900_000, AccountID: revenue}})
	if err != nil {
		t.Fatal(err)
	}
	if err = testutil.SendInvoice(db, invID, 1); err != nil {
		t.Fatal(err)
	}
	m := creditnote.New(db)
	actor := creditnote.Actor{UserID: 1, CanManage: true}
	makeDraft := func(amount, tax int) creditnote.Draft {
		return creditnote.Draft{ContactID: contact, InvoiceID: &invID, Date: "2026-08-02", Reason: model.CreditNoteReasonOther, TaxAmount: tax, Lines: []creditnote.Line{{Description: "Credit", Quantity: 100, UnitPrice: amount, AccountID: revenue}}}
	}
	taxNote, err := m.Create(context.Background(), actor, makeDraft(100_000, 100_001))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Issue(context.Background(), actor, taxNote.ID); err == nil {
		t.Fatal("expected tax ceiling rejection")
	}
	first, err := m.Create(context.Background(), actor, makeDraft(400_000, 60_000))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Issue(context.Background(), actor, first.ID); err != nil {
		t.Fatal(err)
	}
	overTax, err := m.Create(context.Background(), actor, makeDraft(400_000, 50_000))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Issue(context.Background(), actor, overTax.ID); err == nil {
		t.Fatal("expected cumulative tax ceiling rejection")
	}
	second, err := m.Create(context.Background(), actor, makeDraft(500_000, 40_000))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Issue(context.Background(), actor, second.ID); err != nil {
		t.Fatal(err)
	}
	var taxDebit int
	db.QueryRow(`SELECT COALESCE(SUM(jl.debit),0) FROM journal_lines jl JOIN journal_entries je ON je.id=jl.entry_id JOIN accounts a ON a.id=jl.account_id WHERE je.source_type='credit_note' AND je.source_id=? AND a.code=?`, second.ID, model.AccountCodeTax).Scan(&taxDebit)
	if taxDebit != 40_000 {
		t.Fatalf("tax reversal debit=%d", taxDebit)
	}
	third, err := m.Create(context.Background(), actor, makeDraft(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	var conflict *creditnote.ConflictError
	if _, err = m.Issue(context.Background(), actor, third.ID); !errors.As(err, &conflict) {
		t.Fatalf("credit after settlement: %v", err)
	}
}

func TestRejectedIssueLeavesNoOrphanJournal(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES ('A','customer',1),('B','customer',1)")
	var a, b, revenue int
	db.QueryRow("SELECT id FROM contacts WHERE name='A'").Scan(&a)
	db.QueryRow("SELECT id FROM contacts WHERE name='B'").Scan(&b)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenue)
	invoiceID, err := testutil.CreateInvoice(db, &model.Invoice{ContactID: a, InvoiceDate: "2026-08-01", DueDate: "2026-08-31", CreatedBy: 1}, []model.InvoiceLine{{Description: "Service", Quantity: 100, UnitPrice: 1_000_000, AccountID: revenue}})
	if err != nil {
		t.Fatal(err)
	}
	if err = testutil.SendInvoice(db, invoiceID, 1); err != nil {
		t.Fatal(err)
	}
	m := creditnote.New(db)
	actor := creditnote.Actor{UserID: 1, CanManage: true}
	created, err := m.Create(context.Background(), actor, creditnote.Draft{ContactID: b, InvoiceID: &invoiceID, Date: "2026-08-02", Reason: model.CreditNoteReasonOther, Lines: []creditnote.Line{{Description: "Invalid", Quantity: 100, UnitPrice: 100_000, AccountID: revenue}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Issue(context.Background(), actor, created.ID); err == nil {
		t.Fatal("expected contact mismatch")
	}
	got, err := m.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusDraft || got.JournalID != nil {
		t.Fatalf("credit note changed after rejection: %+v", got)
	}
	var journals int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE source_type='credit_note'").Scan(&journals)
	if journals != 0 {
		t.Fatalf("orphan journals=%d", journals)
	}
}

func TestVoidFailureRollsBackReversalStatusAndInvoice(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES ('Void rollback','customer',1)")
	var contact, revenue int
	db.QueryRow("SELECT id FROM contacts WHERE name='Void rollback'").Scan(&contact)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenue)
	invoiceID, err := testutil.CreateInvoice(db, &model.Invoice{ContactID: contact, InvoiceDate: "2026-08-01", DueDate: "2026-08-31", CreatedBy: 1}, []model.InvoiceLine{{Description: "Service", Quantity: 100, UnitPrice: 1_000_000, AccountID: revenue}})
	if err != nil {
		t.Fatal(err)
	}
	if err = testutil.SendInvoice(db, invoiceID, 1); err != nil {
		t.Fatal(err)
	}
	m := creditnote.New(db)
	actor := creditnote.Actor{UserID: 1, CanManage: true}
	created, err := m.Create(context.Background(), actor, creditnote.Draft{ContactID: contact, InvoiceID: &invoiceID, Date: "2026-08-02", Reason: model.CreditNoteReasonCancellation, Lines: []creditnote.Line{{Description: "Cancel", Quantity: 100, UnitPrice: 1_000_000, AccountID: revenue}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Issue(context.Background(), actor, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TRIGGER fail_credit_void BEFORE UPDATE OF status ON credit_notes
		WHEN NEW.status='void' BEGIN SELECT RAISE(ABORT, 'forced void failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Void(context.Background(), actor, created.ID); err == nil {
		t.Fatal("expected void failure")
	}
	got, err := m.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusIssued {
		t.Fatalf("status=%s", got.Status)
	}
	invoice, err := testutil.GetInvoice(db, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Status != model.StatusCancelled || invoice.AmountCredited != 1_000_000 {
		t.Fatalf("invoice changed after failed void: %+v", invoice)
	}
	var journals int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE source_type='credit_note' AND source_id=?", created.ID).Scan(&journals)
	if journals != 1 {
		t.Fatalf("journals=%d; want issue-only 1", journals)
	}
}

func TestPartialPaymentCreditIssueAndVoidRecomputeInvoice(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES ('Partial credit','customer',1)")
	var contact, revenue, cash int
	db.QueryRow("SELECT id FROM contacts WHERE name='Partial credit'").Scan(&contact)
	db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenue)
	db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cash)
	invoiceID, err := testutil.CreateInvoice(db, &model.Invoice{ContactID: contact, InvoiceDate: "2026-08-01", DueDate: "2026-08-31", CreatedBy: 1}, []model.InvoiceLine{{Description: "Service", Quantity: 100, UnitPrice: 1_000_000, AccountID: revenue}})
	if err != nil {
		t.Fatal(err)
	}
	if err = testutil.SendInvoice(db, invoiceID, 1); err != nil {
		t.Fatal(err)
	}
	if err = testutil.RecordInvoicePayment(db, invoiceID, 400_000, "2026-08-02", cash, 1); err != nil {
		t.Fatal(err)
	}
	m := creditnote.New(db)
	actor := creditnote.Actor{UserID: 1, CanManage: true}
	created, err := m.Create(context.Background(), actor, creditnote.Draft{ContactID: contact, InvoiceID: &invoiceID, Date: "2026-08-03", Reason: model.CreditNoteReasonDiscount, Lines: []creditnote.Line{{Description: "Credit", Quantity: 100, UnitPrice: 600_000, AccountID: revenue}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Issue(context.Background(), actor, created.ID); err != nil {
		t.Fatal(err)
	}
	invoice, err := testutil.GetInvoice(db, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Status != model.StatusPaid || invoice.AmountCredited != 600_000 {
		t.Fatalf("invoice after issue: %+v", invoice)
	}
	if _, err = m.Void(context.Background(), actor, created.ID); err != nil {
		t.Fatal(err)
	}
	invoice, err = testutil.GetInvoice(db, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Status != model.StatusPartial || invoice.AmountCredited != 0 || invoice.AmountPaid != 400_000 {
		t.Fatalf("invoice after void: %+v", invoice)
	}
}

func TestIssueRejectsNonCreditableInvoiceStates(t *testing.T) {
	for _, status := range []string{model.StatusDraft, model.StatusCancelled, model.StatusPaid} {
		status := status
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			name := "State " + status
			if _, err := db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES (?,'customer',1)", name); err != nil {
				t.Fatal(err)
			}
			var contact, revenue int
			db.QueryRow("SELECT id FROM contacts WHERE name=?", name).Scan(&contact)
			db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&revenue)
			invoiceID, err := testutil.CreateInvoice(db, &model.Invoice{ContactID: contact, InvoiceDate: "2026-08-01", DueDate: "2026-08-31", CreatedBy: 1}, []model.InvoiceLine{{Description: "Service", Quantity: 100, UnitPrice: 1_000_000, AccountID: revenue}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("UPDATE invoices SET status=? WHERE id=?", status, invoiceID); err != nil {
				t.Fatal(err)
			}
			m := creditnote.New(db)
			actor := creditnote.Actor{UserID: 1, CanManage: true}
			cn, err := m.Create(context.Background(), actor, creditnote.Draft{ContactID: contact, InvoiceID: &invoiceID, Date: "2026-08-02", Reason: model.CreditNoteReasonOther, Lines: []creditnote.Line{{Description: "Credit", Quantity: 100, UnitPrice: 100_000, AccountID: revenue}}})
			if err != nil {
				t.Fatal(err)
			}
			var conflict *creditnote.ConflictError
			if _, err = m.Issue(context.Background(), actor, cn.ID); !errors.As(err, &conflict) {
				t.Fatalf("issue against %s invoice: %v", status, err)
			}
			got, err := m.Get(context.Background(), cn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != model.StatusDraft || got.JournalID != nil {
				t.Fatalf("credit note changed: %+v", got)
			}
		})
	}
}
