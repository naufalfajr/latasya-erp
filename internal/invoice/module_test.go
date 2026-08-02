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
