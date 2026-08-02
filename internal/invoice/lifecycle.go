package invoice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type Payment struct {
	Amount    int
	Date      string
	AccountID int
}

func (m *Module) Send(ctx context.Context, actor Actor, id int) (*model.Invoice, error) {
	return m.send(ctx, actor, id, true)
}

func (m *Module) send(ctx context.Context, actor Actor, id int, logAudit bool) (*model.Invoice, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := m.checkStatus(ctx, id, model.StatusDraft, "can only send draft invoices"); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin invoice send: %w", err)
	}
	defer tx.Rollback()

	locked, err := tx.ExecContext(ctx, "UPDATE invoices SET id=id WHERE id=? AND status=?", id, model.StatusDraft)
	if err != nil {
		return nil, fmt.Errorf("lock invoice send: %w", err)
	}
	n, err := locked.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("sent invoice rows: %w", err)
	}
	if n == 0 {
		return nil, &ConflictError{Message: "invoice is no longer sendable"}
	}
	inv, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}

	var arAccountID int
	if err := tx.QueryRowContext(ctx, "SELECT id FROM accounts WHERE code=?", model.AccountCodeAR).Scan(&arAccountID); err != nil {
		return nil, fmt.Errorf("accounts receivable account not found: %w", err)
	}
	journalLines := []journalLine{{AccountID: arAccountID, Debit: inv.Total, Memo: inv.InvoiceNumber}}
	for _, line := range inv.Lines {
		journalLines = append(journalLines, journalLine{AccountID: line.AccountID, Credit: line.Amount, Memo: line.Description})
	}
	if inv.TaxAmount > 0 {
		var taxAccountID int
		if err := tx.QueryRowContext(ctx, "SELECT id FROM accounts WHERE code=?", model.AccountCodeTax).Scan(&taxAccountID); err == nil {
			journalLines = append(journalLines, journalLine{AccountID: taxAccountID, Credit: inv.TaxAmount, Memo: "Tax"})
		}
	}
	journalID, err := insertJournal(ctx, tx, inv.InvoiceDate,
		fmt.Sprintf("Invoice %s - %s", inv.InvoiceNumber, inv.ContactName), actor.UserID, journalLines)
	if err != nil {
		return nil, fmt.Errorf("create journal entry: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE invoices SET status=?, journal_id=?, updated_at=datetime('now') WHERE id=?", model.StatusSent, journalID, id); err != nil {
		return nil, fmt.Errorf("mark invoice sent: %w", err)
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit invoice send: %w", err)
	}
	if logAudit {
		audit.Log(ctx, m.db, audit.Event{Action: "invoice.send", TargetType: "invoice", TargetID: int64(id),
			TargetLabel: updated.InvoiceNumber, ActorID: int64(actor.UserID),
			Metadata: map[string]any{"after": map[string]any{"status": updated.Status}, "journal_id": updated.JournalID}})
	}
	return updated, nil
}

func (m *Module) CheckPayable(ctx context.Context, actor Actor, id int) error {
	if err := requireManager(actor); err != nil {
		return err
	}
	var status string
	err := m.db.QueryRowContext(ctx, "SELECT status FROM invoices WHERE id=?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check payable invoice: %w", err)
	}
	if status == model.StatusDraft || status == "cancelled" || status == model.StatusPaid {
		return &ConflictError{Message: "cannot record payment for " + status + " invoice"}
	}
	return nil
}

func (m *Module) CheckExists(ctx context.Context, actor Actor, id int) error {
	if err := requireManager(actor); err != nil {
		return err
	}
	var exists int
	err := m.db.QueryRowContext(ctx, "SELECT 1 FROM invoices WHERE id=?", id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check invoice exists: %w", err)
	}
	return nil
}

func (m *Module) RecordPayment(ctx context.Context, actor Actor, id int, payment Payment) (*model.Invoice, error) {
	if err := m.CheckExists(ctx, actor, id); err != nil {
		return nil, err
	}
	fields := map[string]string{}
	if payment.Amount <= 0 {
		fields["amount"] = "must be positive"
	}
	if payment.Date == "" {
		fields["payment_date"] = "required"
	}
	if payment.AccountID <= 0 {
		fields["payment_account"] = "required"
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	if err := m.CheckPayable(ctx, actor, id); err != nil {
		return nil, err
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin invoice payment: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE invoices SET id=id WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("lock invoice payment: %w", err)
	}
	inv, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if inv.Status == model.StatusDraft || inv.Status == "cancelled" || inv.Status == model.StatusPaid {
		return nil, &ConflictError{Message: "cannot record payment for " + inv.Status + " invoice"}
	}
	remaining := inv.Total - inv.AmountPaid - inv.AmountCredited
	if payment.Amount > remaining {
		return nil, &ValidationError{Message: fmt.Sprintf("payment amount (%d) exceeds remaining balance (%d)", payment.Amount, remaining), Fields: map[string]string{"amount": "exceeds remaining balance"}}
	}
	var arAccountID int
	if err := tx.QueryRowContext(ctx, "SELECT id FROM accounts WHERE code=?", model.AccountCodeAR).Scan(&arAccountID); err != nil {
		return nil, fmt.Errorf("accounts receivable account not found: %w", err)
	}
	journalID, err := insertJournal(ctx, tx, payment.Date, "Payment for "+inv.InvoiceNumber, actor.UserID, []journalLine{
		{AccountID: payment.AccountID, Debit: payment.Amount, Memo: "Payment received"},
		{AccountID: arAccountID, Credit: payment.Amount, Memo: inv.InvoiceNumber},
	})
	if err != nil {
		return nil, fmt.Errorf("create payment journal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO payments
		(payment_date, amount, payment_type, reference_id, payment_method, account_id, journal_id, created_by)
		VALUES (?, ?, 'invoice', ?, 'bank_transfer', ?, ?, ?)`, payment.Date, payment.Amount, id, payment.AccountID, journalID, actor.UserID); err != nil {
		return nil, fmt.Errorf("insert payment: %w", err)
	}
	newPaid := inv.AmountPaid + payment.Amount
	status := "partial"
	if newPaid+inv.AmountCredited >= inv.Total {
		status = model.StatusPaid
	}
	if _, err := tx.ExecContext(ctx, "UPDATE invoices SET amount_paid=?, status=?, updated_at=datetime('now') WHERE id=?", newPaid, status, id); err != nil {
		return nil, fmt.Errorf("update invoice payment: %w", err)
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit invoice payment: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "invoice.payment", TargetType: "invoice", TargetID: int64(id), TargetLabel: updated.InvoiceNumber,
		ActorID: int64(actor.UserID), Metadata: map[string]any{"amount": payment.Amount, "payment_date": payment.Date,
			"payment_account_id": payment.AccountID, "status_after": updated.Status}})
	return updated, nil
}

func (m *Module) Delete(ctx context.Context, actor Actor, id int) (*model.Invoice, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := m.checkStatus(ctx, id, model.StatusDraft, "can only delete draft invoices"); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin invoice delete: %w", err)
	}
	defer tx.Rollback()
	locked, err := tx.ExecContext(ctx, "UPDATE invoices SET id=id WHERE id=? AND status=?", id, model.StatusDraft)
	if err != nil {
		return nil, fmt.Errorf("lock invoice delete: %w", err)
	}
	n, err := locked.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("deleted invoice rows: %w", err)
	}
	if n == 0 {
		return nil, &ConflictError{Message: "invoice is no longer deletable"}
	}
	existing, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM invoices WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("delete invoice: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit invoice delete: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "invoice.delete", TargetType: "invoice", TargetID: int64(id), TargetLabel: existing.InvoiceNumber,
		ActorID: int64(actor.UserID), Metadata: map[string]any{"before": map[string]any{"contact_id": existing.ContactID,
			"invoice_date": existing.InvoiceDate, "status": existing.Status, "total": existing.Total}}})
	return existing, nil
}

func (m *Module) checkStatus(ctx context.Context, id int, want, action string) error {
	var status string
	err := m.db.QueryRowContext(ctx, "SELECT status FROM invoices WHERE id=?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != want {
		return &ConflictError{Message: fmt.Sprintf("%s (current: %s)", action, status)}
	}
	return nil
}

type journalLine struct {
	AccountID, Debit, Credit int
	Memo                     string
}

func insertJournal(ctx context.Context, tx *sql.Tx, date, description string, userID int, lines []journalLine) (int, error) {
	var debit, credit int
	for _, line := range lines {
		debit += line.Debit
		credit += line.Credit
	}
	if debit != credit {
		return 0, fmt.Errorf("debits (%d) must equal credits (%d)", debit, credit)
	}
	if debit == 0 {
		return 0, errors.New("journal entry must have at least one debit and credit line")
	}
	prefix := fmt.Sprintf("JE-%s", time.Now().Format("200601"))
	result, err := tx.ExecContext(ctx, `INSERT INTO journal_entries
		(entry_date, reference, description, source_type, source_id, is_posted, created_by)
		SELECT ?, printf('%s-%04d', ?, COALESCE(MAX(CAST(SUBSTR(reference, ?) AS INTEGER)), 0)+1), ?, 'invoice', NULL, 1, ?
		FROM journal_entries WHERE reference LIKE ?`, date, prefix, len(prefix)+2, description, userID, prefix+"-%")
	if err != nil {
		return 0, err
	}
	id64, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	id := int(id64)
	for _, line := range lines {
		if _, err := tx.ExecContext(ctx, "INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo) VALUES (?, ?, ?, ?, ?)",
			id, line.AccountID, line.Debit, line.Credit, line.Memo); err != nil {
			return 0, err
		}
	}
	return id, nil
}
