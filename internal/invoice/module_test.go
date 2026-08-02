package invoice_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestCreateReturnsCompleteInvoiceAndAudit(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := invoice.New(db)
	contactID, accountID := invoiceFixtures(t, db)

	created, err := module.Create(context.Background(), invoice.Actor{UserID: 1, CanManage: true}, invoice.Draft{
		ContactID: contactID, InvoiceDate: "2026-08-02", DueDate: "2026-08-12", TaxAmount: 25_000,
		Lines: []invoice.DraftLine{{Description: "School transport", Quantity: 150, UnitPrice: 200_000, AccountID: accountID}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 || created.InvoiceNumber == "" || len(created.Lines) != 1 {
		t.Fatalf("incomplete result: %#v", created)
	}
	if created.Subtotal != 300_000 || created.Total != 325_000 || created.Lines[0].Amount != 300_000 {
		t.Fatalf("totals: subtotal=%d total=%d line=%d", created.Subtotal, created.Total, created.Lines[0].Amount)
	}

	var auditCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='invoice.create' AND target_id=?", created.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count=%d want 1", auditCount)
	}
	var actorID int
	if err := db.QueryRow("SELECT actor_id FROM audit_log WHERE action='invoice.create' AND target_id=?", created.ID).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if actorID != 1 {
		t.Fatalf("audit actor_id=%d want 1", actorID)
	}
}

func TestCreateAllocatesUniqueNumbersAcrossModuleInstances(t *testing.T) {
	prime := testutil.SetupTestDB(t)
	prime.Close()
	dbPath := filepath.Join(t.TempDir(), "invoices.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Seed(db); err != nil {
		t.Fatal(err)
	}
	db2, err := database.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db2.Close() })
	contactID, accountID := invoiceFixtures(t, db)
	actor := invoice.Actor{UserID: 1, CanManage: true}
	draft := invoice.Draft{
		ContactID: contactID, InvoiceDate: "2026-08-02", DueDate: "2026-08-12",
		Lines: []invoice.DraftLine{{Description: "Concurrent", Quantity: 100, UnitPrice: 100_000, AccountID: accountID}},
	}

	modules := []*invoice.Module{invoice.New(db), invoice.New(db2)}
	results := make(chan *model.Invoice, len(modules))
	errs := make(chan error, len(modules))
	var wg sync.WaitGroup
	for _, module := range modules {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := module.Create(context.Background(), actor, draft)
			if err != nil {
				errs <- err
				return
			}
			results <- created
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Errorf("concurrent Create: %v", err)
	}
	numbers := map[string]bool{}
	for created := range results {
		if numbers[created.InvoiceNumber] {
			t.Errorf("duplicate invoice number %q", created.InvoiceNumber)
		}
		numbers[created.InvoiceNumber] = true
	}
	if len(numbers) != len(modules) {
		t.Fatalf("created %d invoices, want %d: %v", len(numbers), len(modules), fmt.Sprint(numbers))
	}
}

func TestCreateRejectsForbiddenAndInvalidDrafts(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := invoice.New(db)

	_, err := module.Create(context.Background(), invoice.Actor{UserID: 2}, invoice.Draft{})
	if !errors.Is(err, invoice.ErrForbidden) {
		t.Fatalf("error=%v want ErrForbidden", err)
	}

	_, err = module.Create(context.Background(), invoice.Actor{UserID: 1, CanManage: true}, invoice.Draft{})
	var validation *invoice.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error=%v want ValidationError", err)
	}
	for _, field := range []string{"contact_id", "invoice_date", "due_date", "lines"} {
		if validation.Fields[field] == "" {
			t.Errorf("missing validation field %q", field)
		}
	}
}

func TestUpdateReturnsCompleteInvoiceAndTypedErrors(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := invoice.New(db)
	contactID, accountID := invoiceFixtures(t, db)
	actor := invoice.Actor{UserID: 1, CanManage: true}

	created, err := module.Create(context.Background(), actor, invoice.Draft{
		ContactID: contactID, InvoiceDate: "2026-08-02", DueDate: "2026-08-12",
		Lines: []invoice.DraftLine{{Description: "Original", Quantity: 100, UnitPrice: 100_000, AccountID: accountID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := module.Update(context.Background(), actor, created.ID, invoice.Draft{
		ContactID: contactID, InvoiceDate: "2026-08-03", DueDate: "2026-08-15",
		Lines: []invoice.DraftLine{{Description: "Updated", Quantity: 200, UnitPrice: 150_000, AccountID: accountID}},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Total != 300_000 || updated.Lines[0].Description != "Updated" {
		t.Fatalf("updated result: %#v", updated)
	}

	if _, err := db.Exec("UPDATE invoices SET status=? WHERE id=?", model.StatusSent, created.ID); err != nil {
		t.Fatal(err)
	}
	_, err = module.Update(context.Background(), actor, created.ID, invoice.Draft{
		ContactID: contactID, InvoiceDate: "2026-08-03", DueDate: "2026-08-15",
		Lines: []invoice.DraftLine{{Description: "Again", Quantity: 100, UnitPrice: 100_000, AccountID: accountID}},
	})
	var conflict *invoice.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error=%v want ConflictError", err)
	}

	_, err = module.Update(context.Background(), actor, 999999, invoice.Draft{
		ContactID: contactID, InvoiceDate: "2026-08-03", DueDate: "2026-08-15",
		Lines: []invoice.DraftLine{{Description: "Missing", Quantity: 100, UnitPrice: 100_000, AccountID: accountID}},
	})
	if !errors.Is(err, invoice.ErrNotFound) {
		t.Fatalf("error=%v want ErrNotFound", err)
	}
}

func TestSendPaymentAndDeleteLifecycle(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := invoice.New(db)
	contactID, accountID := invoiceFixtures(t, db)
	actor := invoice.Actor{UserID: 1, CanManage: true}
	create := func(description string) *model.Invoice {
		t.Helper()
		created, err := module.Create(context.Background(), actor, invoice.Draft{
			ContactID: contactID, InvoiceDate: "2026-08-02", DueDate: "2026-08-12",
			Lines: []invoice.DraftLine{{Description: description, Quantity: 100, UnitPrice: 200_000, AccountID: accountID}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return created
	}

	toSend := create("Send")
	sent, err := module.Send(context.Background(), actor, toSend.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Status != model.StatusSent || sent.JournalID == nil {
		t.Fatalf("sent=%#v", sent)
	}
	if _, err := module.Send(context.Background(), actor, toSend.ID); err == nil {
		t.Fatal("second Send succeeded")
	}

	var cashID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cashID); err != nil {
		t.Fatal(err)
	}
	paid, err := module.RecordPayment(context.Background(), actor, sent.ID, invoice.Payment{Amount: 200_000, Date: "2026-08-03", AccountID: cashID})
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != model.StatusPaid || paid.AmountPaid != 200_000 {
		t.Fatalf("paid=%#v", paid)
	}

	toDelete := create("Delete")
	deleted, err := module.Delete(context.Background(), actor, toDelete.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != toDelete.ID {
		t.Fatalf("deleted id=%d want %d", deleted.ID, toDelete.ID)
	}
	if _, err := module.Delete(context.Background(), actor, toDelete.ID); !errors.Is(err, invoice.ErrNotFound) {
		t.Fatalf("second Delete error=%v want not found", err)
	}
}

func TestInvoiceQueriesReturnCompletePresentationData(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := invoice.New(db)
	contactID, accountID := invoiceFixtures(t, db)
	created, err := module.Create(context.Background(), invoice.Actor{UserID: 1, CanManage: true}, invoice.Draft{
		ContactID: contactID, InvoiceDate: "2026-08-02", DueDate: "2026-08-12",
		Lines: []invoice.DraftLine{{Description: "Query", Quantity: 100, UnitPrice: 100_000, AccountID: accountID}},
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := module.List(context.Background(), invoice.Filter{Status: model.StatusDraft, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Invoices) != 1 {
		t.Fatalf("list=%#v", list)
	}
	detail, err := module.Detail(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Invoice.ID != created.ID || detail.CreditNotes == nil {
		t.Fatalf("detail=%#v", detail)
	}
	view, err := module.View(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.AssetAccounts) == 0 {
		t.Fatalf("view=%#v", view)
	}
	options, err := module.FormOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Contacts) == 0 || len(options.RevenueAccounts) == 0 {
		t.Fatalf("options=%#v", options)
	}
	document, err := module.Document(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if document.Company == nil || document.Invoice.ID != created.ID {
		t.Fatalf("document=%#v", document)
	}
	if _, err := module.Get(context.Background(), 999999); !errors.Is(err, invoice.ErrNotFound) {
		t.Fatalf("Get missing=%v", err)
	}
}

func TestPortalInvoicesExcludeDrafts(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := invoice.New(db)
	contactID, accountID := invoiceFixtures(t, db)
	actor := invoice.Actor{UserID: 1, CanManage: true}
	create := func(date string) *model.Invoice {
		t.Helper()
		created, err := module.Create(context.Background(), actor, invoice.Draft{
			ContactID: contactID, InvoiceDate: date, DueDate: date,
			Lines: []invoice.DraftLine{{Description: "Portal", Quantity: 100, UnitPrice: 400_000, AccountID: accountID}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	create("2026-07-01")
	sent := create("2026-06-01")
	if _, err := module.Send(context.Background(), actor, sent.ID); err != nil {
		t.Fatal(err)
	}

	invoices, err := module.PortalInvoices(context.Background(), []int{contactID})
	if err != nil {
		t.Fatal(err)
	}
	if len(invoices) != 1 || invoices[0].ID != sent.ID {
		t.Fatalf("portal invoices=%#v want sent invoice %d", invoices, sent.ID)
	}
}

func TestBatchAndShareUseCasesEnforceModuleContract(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := invoice.New(db)
	contactID, accountID := invoiceFixtures(t, db)
	if _, err := db.Exec("UPDATE contacts SET phone='08123456789' WHERE id=?", contactID); err != nil {
		t.Fatal(err)
	}
	actor := invoice.Actor{UserID: 1, CanManage: true}
	create := func(label string) *model.Invoice {
		t.Helper()
		created, err := module.Create(context.Background(), actor, invoice.Draft{
			ContactID: contactID, InvoiceDate: "2026-08-02", DueDate: "2026-08-12",
			Lines: []invoice.DraftLine{{Description: label, Quantity: 100, UnitPrice: 100_000, AccountID: accountID}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return created
	}

	toDelete, sent, toBulkSend := create("Delete"), create("Sent"), create("Bulk send")
	if _, err := module.Send(context.Background(), actor, sent.ID); err != nil {
		t.Fatal(err)
	}

	deleted, err := module.BulkDelete(context.Background(), actor, []int{toDelete.ID, sent.ID, 999999})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.Deleted) != 1 || deleted.Deleted[0].ID != toDelete.ID || len(deleted.Skipped) != 2 {
		t.Fatalf("bulk delete=%#v", deleted)
	}

	bulkSent, err := module.BulkSend(context.Background(), actor, []int{toBulkSend.ID, sent.ID, 999999})
	if err != nil {
		t.Fatal(err)
	}
	if len(bulkSent.Sent) != 1 || bulkSent.Sent[0].ID != toBulkSend.ID || bulkSent.Sent[0].JournalID == nil || len(bulkSent.Skipped) != 2 {
		t.Fatalf("bulk send=%#v", bulkSent)
	}

	share, err := module.PrepareShare(context.Background(), actor, sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if share.Phone == "" || share.PortalCode == "" || share.InvoiceNumber != sent.InvoiceNumber {
		t.Fatalf("share=%#v", share)
	}

	for name, invoke := range map[string]func() error{
		"bulk delete": func() error {
			_, err := module.BulkDelete(context.Background(), invoice.Actor{}, []int{sent.ID})
			return err
		},
		"bulk send": func() error {
			_, err := module.BulkSend(context.Background(), invoice.Actor{}, []int{sent.ID})
			return err
		},
		"recurring": func() error {
			_, err := module.GenerateRecurring(context.Background(), invoice.Actor{}, "2026-08-02", "2026-08-12")
			return err
		},
		"share": func() error {
			_, err := module.PrepareShare(context.Background(), invoice.Actor{}, sent.ID)
			return err
		},
	} {
		if err := invoke(); !errors.Is(err, invoice.ErrForbidden) {
			t.Errorf("%s error=%v want ErrForbidden", name, err)
		}
	}

	for _, action := range []string{"invoice.bulk_delete", "invoice.bulk_send"} {
		var actorID int
		if err := db.QueryRow("SELECT actor_id FROM audit_log WHERE action=? ORDER BY id DESC LIMIT 1", action).Scan(&actorID); err != nil {
			t.Fatalf("%s audit: %v", action, err)
		}
		if actorID != actor.UserID {
			t.Errorf("%s actor=%d want %d", action, actorID, actor.UserID)
		}
	}
}

func TestGenerateRecurringIsAtomicAcrossModuleInstances(t *testing.T) {
	prime := testutil.SetupTestDB(t)
	prime.Close()
	dbPath := filepath.Join(t.TempDir(), "recurring.db")
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
	if _, err := db1.Exec("INSERT INTO contacts (name, contact_type, is_active, distance_km) VALUES ('Recurring', 'customer', 1, 5)"); err != nil {
		t.Fatal(err)
	}

	actor := invoice.Actor{UserID: 1, CanManage: true}
	results := make(chan *invoice.RecurringResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, module := range []*invoice.Module{invoice.New(db1), invoice.New(db2)} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := module.GenerateRecurring(context.Background(), actor, "2026-08-03", "2026-08-13")
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Errorf("GenerateRecurring: %v", err)
	}
	created, skipped := 0, 0
	for result := range results {
		created += result.Created
		skipped += result.Skipped
	}
	if created != 1 || skipped != 1 {
		t.Fatalf("created=%d skipped=%d want 1/1", created, skipped)
	}
	var invoices, claims int
	if err := db1.QueryRow("SELECT COUNT(*) FROM invoices WHERE substr(invoice_date,1,7)='2026-08'").Scan(&invoices); err != nil {
		t.Fatal(err)
	}
	if err := db1.QueryRow("SELECT COUNT(*) FROM invoice_recurring_claims WHERE invoice_month='2026-08'").Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if invoices != 1 || claims != 1 {
		t.Fatalf("invoices=%d claims=%d want 1/1", invoices, claims)
	}
}

func TestUpdateMovesRecurringClaimWithDraft(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := invoice.New(db)
	actor := invoice.Actor{UserID: 1, CanManage: true}
	firstID, accountID := invoiceFixtures(t, db)
	second, err := db.Exec("INSERT INTO contacts (name, contact_type, is_active, distance_km) VALUES ('Moved Customer', 'customer', 1, 5)")
	if err != nil {
		t.Fatal(err)
	}
	secondID64, _ := second.LastInsertId()
	secondID := int(secondID64)

	result, err := module.GenerateRecurring(context.Background(), actor, "2026-08-03", "2026-08-13")
	if err != nil {
		t.Fatal(err)
	}
	var generatedID int
	for _, item := range result.Items {
		if item.ContactID == firstID {
			generatedID = item.InvoiceID
		}
	}
	if generatedID == 0 {
		t.Fatalf("first customer not generated: %#v", result)
	}
	if _, err := module.Update(context.Background(), actor, generatedID, invoice.Draft{
		ContactID: secondID, InvoiceDate: "2026-09-03", DueDate: "2026-09-13",
		Lines: []invoice.DraftLine{{Description: "Moved recurring", Quantity: 100, UnitPrice: 100_000, AccountID: accountID}},
	}); err != nil {
		t.Fatal(err)
	}

	var oldClaims, newClaims int
	if err := db.QueryRow("SELECT COUNT(*) FROM invoice_recurring_claims WHERE contact_id=? AND invoice_month='2026-08'", firstID).Scan(&oldClaims); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM invoice_recurring_claims WHERE contact_id=? AND invoice_month='2026-09' AND invoice_id=?", secondID, generatedID).Scan(&newClaims); err != nil {
		t.Fatal(err)
	}
	if oldClaims != 0 || newClaims != 1 {
		t.Fatalf("old claims=%d new claims=%d want 0/1", oldClaims, newClaims)
	}

	again, err := module.GenerateRecurring(context.Background(), actor, "2026-08-03", "2026-08-13")
	if err != nil {
		t.Fatal(err)
	}
	var recreated bool
	for _, item := range again.Items {
		if item.ContactID == firstID && item.Result == invoice.GeneratedCreated {
			recreated = true
		}
	}
	if !recreated {
		t.Fatalf("original customer/month remained blocked: %#v", again)
	}
}

func invoiceFixtures(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	result, err := db.Exec("INSERT INTO contacts (name, contact_type) VALUES ('Module Customer', 'customer')")
	if err != nil {
		t.Fatal(err)
	}
	contactID, _ := result.LastInsertId()
	var accountID int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code='4-1001'").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	return int(contactID), accountID
}
