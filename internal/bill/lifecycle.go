package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

func validateDraft(d Draft) error {
	fields := map[string]string{}
	if d.ContactID <= 0 {
		fields["contact_id"] = "required"
	}
	if strings.TrimSpace(d.BillDate) == "" {
		fields["bill_date"] = "required"
	}
	if strings.TrimSpace(d.DueDate) == "" {
		fields["due_date"] = "required"
	}
	if d.TaxAmount < 0 {
		fields["tax_amount"] = "must be non-negative"
	}
	if len(d.Lines) == 0 {
		fields["lines"] = "at least one line required"
	}
	for i, l := range d.Lines {
		prefix := "lines[" + strconv.Itoa(i) + "]"
		if strings.TrimSpace(l.Description) == "" {
			fields[prefix+".description"] = "required"
		}
		if l.Quantity <= 0 {
			fields[prefix+".quantity"] = "must be positive"
		}
		if l.UnitPrice <= 0 {
			fields[prefix+".unit_price"] = "must be positive"
		}
		if l.AccountID <= 0 {
			fields[prefix+".account_id"] = "required"
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func calculated(d Draft) (int, []model.BillLine) {
	lines := make([]model.BillLine, len(d.Lines))
	subtotal := 0
	for i, l := range d.Lines {
		amount := l.Quantity * l.UnitPrice / 100
		subtotal += amount
		lines[i] = model.BillLine{Description: l.Description, Quantity: l.Quantity, UnitPrice: l.UnitPrice, Amount: amount, AccountID: l.AccountID}
	}
	return subtotal, lines
}

func (m *Module) Create(ctx context.Context, actor Actor, d Draft) (*model.Bill, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validateDraft(d); err != nil {
		return nil, err
	}
	subtotal, lines := calculated(d)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bill create: %w", err)
	}
	defer tx.Rollback()
	if err := requireDraftRefs(ctx, tx, d); err != nil {
		return nil, err
	}
	number, err := model.GenerateDocNumberContext(ctx, tx, "bills", "bill_number", "BILL")
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO bills (bill_number,contact_id,bill_date,due_date,status,subtotal,tax_amount,total,amount_paid,notes,created_by) VALUES (?,?,?,?,'draft',?,?,?,0,?,?)`, number, d.ContactID, d.BillDate, d.DueDate, subtotal, d.TaxAmount, subtotal+d.TaxAmount, d.Notes, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("insert bill: %w", err)
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("bill id: %w", err)
	}
	id := int(id64)
	if err := insertBillLines(ctx, tx, id, lines); err != nil {
		return nil, err
	}
	created, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bill create: %w", err)
	}
	m.audit(ctx, actor, "bill.create", created, map[string]any{"after": snapshot(created)})
	return created, nil
}

func (m *Module) Update(ctx context.Context, actor Actor, id int, d Draft) (*model.Bill, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validateDraft(d); err != nil {
		return nil, err
	}
	subtotal, lines := calculated(d)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bill update: %w", err)
	}
	defer tx.Rollback()
	if err := lockBill(ctx, tx, id); err != nil {
		return nil, err
	}
	old, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if old.Status != model.StatusDraft {
		return nil, &ConflictError{Message: fmt.Sprintf("can only edit draft bills (current: %s)", old.Status)}
	}
	if err := requireDraftRefs(ctx, tx, d); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE bills SET contact_id=?,bill_date=?,due_date=?,subtotal=?,tax_amount=?,total=?,notes=?,updated_at=datetime('now') WHERE id=?`, d.ContactID, d.BillDate, d.DueDate, subtotal, d.TaxAmount, subtotal+d.TaxAmount, d.Notes, id)
	if err != nil {
		return nil, fmt.Errorf("update bill: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM bill_lines WHERE bill_id=?", id); err != nil {
		return nil, fmt.Errorf("delete bill lines: %w", err)
	}
	if err = insertBillLines(ctx, tx, id, lines); err != nil {
		return nil, err
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bill update: %w", err)
	}
	if diff := audit.Diff(snapshot(old), snapshot(updated), []string{"contact_id", "bill_date", "due_date", "tax_amount", "notes", "total"}); diff != nil {
		m.audit(ctx, actor, "bill.update", updated, diff)
	}
	return updated, nil
}

func (m *Module) Delete(ctx context.Context, actor Actor, id int) (*model.Bill, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bill delete: %w", err)
	}
	defer tx.Rollback()
	if err = lockBill(ctx, tx, id); err != nil {
		return nil, err
	}
	old, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if old.Status != model.StatusDraft {
		return nil, &ConflictError{Message: fmt.Sprintf("can only delete draft bills (current: %s)", old.Status)}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM bills WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("delete bill: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bill delete: %w", err)
	}
	m.audit(ctx, actor, "bill.delete", old, map[string]any{"before": snapshot(old)})
	return old, nil
}

func (m *Module) Receive(ctx context.Context, actor Actor, id int) (*model.Bill, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bill receive: %w", err)
	}
	defer tx.Rollback()
	if err = lockBill(ctx, tx, id); err != nil {
		return nil, err
	}
	b, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if b.Status != model.StatusDraft {
		return nil, &ConflictError{Message: fmt.Sprintf("can only receive draft bills (current: %s)", b.Status)}
	}
	reference, err := model.GenerateDocNumberContext(ctx, tx, "journal_entries", "reference", "JE")
	if err != nil {
		return nil, err
	}
	ap, err := accountByCode(ctx, tx, model.AccountCodeAP, true)
	if err != nil {
		return nil, err
	}
	lines := make([]journalLine, 0, len(b.Lines)+2)
	for _, l := range b.Lines {
		lines = append(lines, journalLine{l.AccountID, l.Amount, 0, l.Description})
	}
	if b.TaxAmount > 0 {
		tax, err := accountByCode(ctx, tx, model.AccountCodeTax, true)
		if err != nil {
			return nil, err
		}
		lines = append(lines, journalLine{tax, b.TaxAmount, 0, "Tax"})
	}
	lines = append(lines, journalLine{ap, 0, b.Total, b.BillNumber})
	journalID, err := insertJournal(ctx, tx, reference, b.BillDate, fmt.Sprintf("Bill %s - %s", b.BillNumber, b.ContactName), model.SourceBill, id, actor.UserID, lines)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE bills SET status='received',journal_id=?,updated_at=datetime('now') WHERE id=?", journalID, id); err != nil {
		return nil, fmt.Errorf("receive bill: %w", err)
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bill receive: %w", err)
	}
	m.audit(ctx, actor, "bill.receive", updated, map[string]any{"after": map[string]any{"status": updated.Status}, "journal_id": updated.JournalID})
	return updated, nil
}

func (m *Module) RecordPayment(ctx context.Context, actor Actor, id int, p Payment) (*model.Bill, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	fields := map[string]string{}
	if p.Amount <= 0 {
		fields["amount"] = "must be positive"
	}
	if strings.TrimSpace(p.PaymentDate) == "" {
		fields["payment_date"] = "required"
	}
	if p.PaymentAccount <= 0 {
		fields["payment_account"] = "required"
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bill payment: %w", err)
	}
	defer tx.Rollback()
	if err = lockBill(ctx, tx, id); err != nil {
		return nil, err
	}
	b, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if b.Status == model.StatusDraft || b.Status == model.StatusCancelled || b.Status == model.StatusPaid {
		return nil, &ConflictError{Message: fmt.Sprintf("cannot record payment for %s bill", b.Status)}
	}
	remaining := b.Total - b.AmountPaid
	if p.Amount > remaining {
		return nil, &ConflictError{Message: fmt.Sprintf("payment amount (%d) exceeds remaining balance (%d)", p.Amount, remaining)}
	}
	reference, err := model.GenerateDocNumberContext(ctx, tx, "journal_entries", "reference", "JE")
	if err != nil {
		return nil, err
	}
	ap, err := accountByCode(ctx, tx, model.AccountCodeAP, true)
	if err != nil {
		return nil, err
	}
	if err = requireAccountType(ctx, tx, p.PaymentAccount, model.AccountTypeAsset); err != nil {
		return nil, err
	}
	journalID, err := insertJournal(ctx, tx, reference, p.PaymentDate, "Payment for "+b.BillNumber, model.SourceBill, id, actor.UserID, []journalLine{{ap, p.Amount, 0, b.BillNumber}, {p.PaymentAccount, 0, p.Amount, "Payment"}})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO payments (payment_date,amount,payment_type,reference_id,payment_method,account_id,journal_id,created_by) VALUES (?,?,'bill',?,'bank_transfer',?,?,?)`, p.PaymentDate, p.Amount, id, p.PaymentAccount, journalID, actor.UserID); err != nil {
		return nil, fmt.Errorf("insert bill payment: %w", err)
	}
	paid := b.AmountPaid + p.Amount
	status := model.StatusPartial
	if paid >= b.Total {
		status = model.StatusPaid
	}
	if _, err = tx.ExecContext(ctx, "UPDATE bills SET amount_paid=?,status=?,updated_at=datetime('now') WHERE id=?", paid, status, id); err != nil {
		return nil, fmt.Errorf("update bill payment: %w", err)
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bill payment: %w", err)
	}
	m.audit(ctx, actor, "bill.payment", updated, map[string]any{"amount": p.Amount, "payment_date": p.PaymentDate, "payment_account_id": p.PaymentAccount, "status_after": updated.Status})
	return updated, nil
}

func insertBillLines(ctx context.Context, tx *sql.Tx, id int, lines []model.BillLine) error {
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx, `INSERT INTO bill_lines (bill_id,description,quantity,unit_price,amount,account_id) VALUES (?,?,?,?,?,?)`, id, l.Description, l.Quantity, l.UnitPrice, l.Amount, l.AccountID); err != nil {
			return fmt.Errorf("insert bill line: %w", err)
		}
	}
	return nil
}
func lockBill(ctx context.Context, tx *sql.Tx, id int) error {
	res, err := tx.ExecContext(ctx, "UPDATE bills SET id=id WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("lock bill: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type journalLine struct {
	accountID, debit, credit int
	memo                     string
}

func insertJournal(ctx context.Context, tx *sql.Tx, ref, date, description, source string, sourceID, userID int, lines []journalLine) (int, error) {
	debit, credit := 0, 0
	for _, l := range lines {
		if l.accountID <= 0 || l.debit < 0 || l.credit < 0 || (l.debit == 0 && l.credit == 0) || (l.debit > 0 && l.credit > 0) {
			return 0, errors.New("invalid source journal line")
		}
		debit += l.debit
		credit += l.credit
	}
	if debit == 0 || debit != credit {
		return 0, errors.New("journal entry must be balanced and non-zero")
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO journal_entries (entry_date,reference,description,source_type,source_id,is_posted,created_by) VALUES (?,?,?,?,?,1,?)`, date, ref, description, source, sourceID, userID)
	if err != nil {
		return 0, fmt.Errorf("insert bill journal: %w", err)
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	id := int(id64)
	for _, l := range lines {
		if _, err = tx.ExecContext(ctx, `INSERT INTO journal_lines (entry_id,account_id,debit,credit,memo) VALUES (?,?,?,?,?)`, id, l.accountID, l.debit, l.credit, l.memo); err != nil {
			return 0, fmt.Errorf("insert bill journal line: %w", err)
		}
	}
	return id, nil
}
func accountByCode(ctx context.Context, q queryer, code string, required bool) (int, error) {
	var id int
	err := q.QueryRowContext(ctx, "SELECT id FROM accounts WHERE code=?", code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) && required {
		return 0, fmt.Errorf("account %s not found", code)
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}
func requireAccountType(ctx context.Context, q queryer, id int, want string) error {
	var got string
	err := q.QueryRowContext(ctx, "SELECT account_type FROM accounts WHERE id=? AND is_active=1", id).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return &ValidationError{Fields: map[string]string{"payment_account": "active account not found"}}
	}
	if err != nil {
		return err
	}
	if got != want {
		return &ValidationError{Fields: map[string]string{"payment_account": "must be an asset account"}}
	}
	return nil
}

func requireDraftRefs(ctx context.Context, q queryer, d Draft) error {
	fields := map[string]string{}
	var contactType string
	if err := q.QueryRowContext(ctx, "SELECT contact_type FROM contacts WHERE id=? AND is_active=1", d.ContactID).Scan(&contactType); err != nil || contactType != "supplier" {
		fields["contact_id"] = "active supplier not found"
	}
	for i, line := range d.Lines {
		var accountType string
		if err := q.QueryRowContext(ctx, "SELECT account_type FROM accounts WHERE id=? AND is_active=1", line.AccountID).Scan(&accountType); err != nil || accountType != model.AccountTypeExpense {
			fields["lines["+strconv.Itoa(i)+"].account_id"] = "active expense account not found"
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}
func snapshot(b *model.Bill) map[string]any {
	return map[string]any{"contact_id": b.ContactID, "bill_date": b.BillDate, "due_date": b.DueDate, "tax_amount": b.TaxAmount, "notes": b.Notes, "status": b.Status, "total": b.Total, "line_count": len(b.Lines)}
}
func (m *Module) audit(ctx context.Context, actor Actor, action string, b *model.Bill, metadata map[string]any) {
	audit.Log(ctx, m.db, audit.Event{Action: action, TargetType: "bill", TargetID: int64(b.ID), TargetLabel: b.BillNumber, Metadata: metadata, ActorID: int64(actor.UserID)})
}
