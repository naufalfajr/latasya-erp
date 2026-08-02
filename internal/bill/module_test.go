package bill_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/naufal/latasya-erp/internal/bill"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func setup(t *testing.T) (*sql.DB, *bill.Module, int, int, int) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	res, err := db.Exec("INSERT INTO contacts (name,contact_type,is_active) VALUES ('Supplier','supplier',1)")
	if err != nil {
		t.Fatal(err)
	}
	contact64, _ := res.LastInsertId()
	var expense, cash int
	db.QueryRow("SELECT id FROM accounts WHERE code='5-1001'").Scan(&expense)
	db.QueryRow("SELECT id FROM accounts WHERE code='1-1001'").Scan(&cash)
	return db, bill.New(db), int(contact64), expense, cash
}

func TestAuthorizationAndStateGates(t *testing.T) {
	t.Parallel()
	_, m, contact, expense, cash := setup(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, bill.Actor{UserID: 1}, draft(contact, expense)); !errors.Is(err, bill.ErrForbidden) {
		t.Fatalf("forbidden create: %v", err)
	}
	created, err := m.Create(ctx, actor(), draft(contact, expense))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Receive(ctx, actor(), created.ID); err != nil {
		t.Fatal(err)
	}
	var conflict *bill.ConflictError
	if _, err = m.Update(ctx, actor(), created.ID, draft(contact, expense)); !errors.As(err, &conflict) {
		t.Fatalf("update received: %v", err)
	}
	if _, err = m.Delete(ctx, actor(), created.ID); !errors.As(err, &conflict) {
		t.Fatalf("delete received: %v", err)
	}
	if _, err = m.RecordPayment(ctx, actor(), created.ID, bill.Payment{Amount: created.Total + 1, PaymentDate: "2026-08-10", PaymentAccount: cash}); !errors.As(err, &conflict) {
		t.Fatalf("overpayment: %v", err)
	}
	if _, err = m.RecordPayment(ctx, actor(), created.ID, bill.Payment{Amount: created.Total, PaymentDate: "2026-08-10", PaymentAccount: cash}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.RecordPayment(ctx, actor(), created.ID, bill.Payment{Amount: 1, PaymentDate: "2026-08-11", PaymentAccount: cash}); !errors.As(err, &conflict) {
		t.Fatalf("payment after paid: %v", err)
	}
	draftOnly, err := m.Create(ctx, actor(), draft(contact, expense))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Delete(ctx, actor(), draftOnly.ID); err != nil {
		t.Fatalf("delete draft: %v", err)
	}
}
func actor() bill.Actor { return bill.Actor{UserID: 1, CanManage: true} }
func draft(contact, expense int) bill.Draft {
	return bill.Draft{ContactID: contact, BillDate: "2026-08-01", DueDate: "2026-08-31", Lines: []bill.Line{{Description: "Fuel", Quantity: 100, UnitPrice: 2_000_000, AccountID: expense}}}
}

func TestLifecyclePostsBalancedJournalsAndAudit(t *testing.T) {
	t.Parallel()
	db, m, contact, expense, cash := setup(t)
	ctx := context.Background()
	created, err := m.Create(ctx, actor(), draft(contact, expense))
	if err != nil {
		t.Fatal(err)
	}
	received, err := m.Receive(ctx, actor(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if received.Status != model.StatusReceived || received.JournalID == nil {
		t.Fatalf("receive result: %+v", received)
	}
	partial, err := m.RecordPayment(ctx, actor(), created.ID, bill.Payment{Amount: 1_000_000, PaymentDate: "2026-08-10", PaymentAccount: cash})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Status != model.StatusPartial {
		t.Fatalf("status=%s", partial.Status)
	}
	paid, err := m.RecordPayment(ctx, actor(), created.ID, bill.Payment{Amount: 1_000_000, PaymentDate: "2026-08-11", PaymentAccount: cash})
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != model.StatusPaid || paid.AmountDue() != 0 {
		t.Fatalf("paid result: %+v", paid)
	}
	var unbalanced int
	if err := db.QueryRow(`SELECT COUNT(*) FROM journal_entries je WHERE je.source_type='bill' AND (SELECT COALESCE(SUM(debit-credit),0) FROM journal_lines WHERE entry_id=je.id)<>0`).Scan(&unbalanced); err != nil {
		t.Fatal(err)
	}
	if unbalanced != 0 {
		t.Fatalf("unbalanced journals=%d", unbalanced)
	}
	var audits int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE target_type='bill'").Scan(&audits)
	if audits != 4 {
		t.Fatalf("audits=%d", audits)
	}
}

func TestReceiveFailureLeavesNoOrphanJournal(t *testing.T) {
	t.Parallel()
	db, m, contact, expense, _ := setup(t)
	created, err := m.Create(context.Background(), actor(), draft(contact, expense))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DELETE FROM accounts WHERE code=?", model.AccountCodeAP); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Receive(context.Background(), actor(), created.ID); err == nil {
		t.Fatal("expected receive failure")
	}
	got, err := m.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusDraft || got.JournalID != nil {
		t.Fatalf("bill changed after failure: %+v", got)
	}
	var journals int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE source_type='bill'").Scan(&journals)
	if journals != 0 {
		t.Fatalf("orphan journals=%d", journals)
	}
}

func TestPaymentFailureRollsBackJournalPaymentAndBill(t *testing.T) {
	t.Parallel()
	db, m, contact, expense, cash := setup(t)
	created, err := m.Create(context.Background(), actor(), draft(contact, expense))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Receive(context.Background(), actor(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TRIGGER fail_bill_payment BEFORE INSERT ON payments
		WHEN NEW.payment_type='bill' BEGIN SELECT RAISE(ABORT, 'forced payment failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = m.RecordPayment(context.Background(), actor(), created.ID, bill.Payment{Amount: 1_000_000, PaymentDate: "2026-08-10", PaymentAccount: cash}); err == nil {
		t.Fatal("expected payment failure")
	}
	got, err := m.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusReceived || got.AmountPaid != 0 {
		t.Fatalf("bill changed after failure: %+v", got)
	}
	var payments, journals int
	db.QueryRow("SELECT COUNT(*) FROM payments WHERE payment_type='bill' AND reference_id=?", created.ID).Scan(&payments)
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE source_type='bill' AND source_id=?", created.ID).Scan(&journals)
	if payments != 0 || journals != 1 {
		t.Fatalf("payments=%d journals=%d; want 0 and receive-only 1", payments, journals)
	}
}
