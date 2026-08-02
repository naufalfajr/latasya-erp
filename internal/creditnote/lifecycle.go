package creditnote

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

var reasons = map[string]bool{model.CreditNoteReasonCancellation: true, model.CreditNoteReasonReturn: true, model.CreditNoteReasonDiscount: true, model.CreditNoteReasonOther: true}

func validateDraft(d Draft) error {
	fields := map[string]string{}
	if d.ContactID <= 0 {
		fields["contact_id"] = "required"
	}
	if strings.TrimSpace(d.Date) == "" {
		fields["cn_date"] = "required"
	}
	if !reasons[d.Reason] {
		fields["reason"] = "invalid reason"
	}
	if d.TaxAmount < 0 {
		fields["tax_amount"] = "must be non-negative"
	}
	if len(d.Lines) == 0 {
		fields["lines"] = "at least one line required"
	}
	for i, l := range d.Lines {
		p := "lines[" + strconv.Itoa(i) + "]"
		if strings.TrimSpace(l.Description) == "" {
			fields[p+".description"] = "required"
		}
		if l.Quantity <= 0 {
			fields[p+".quantity"] = "must be positive"
		}
		if l.UnitPrice <= 0 {
			fields[p+".unit_price"] = "must be positive"
		}
		if l.AccountID <= 0 {
			fields[p+".account_id"] = "required"
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}
func calculated(d Draft) (int, []model.CreditNoteLine) {
	lines := make([]model.CreditNoteLine, len(d.Lines))
	subtotal := 0
	for i, l := range d.Lines {
		amount := l.Quantity * l.UnitPrice / 100
		subtotal += amount
		lines[i] = model.CreditNoteLine{Description: l.Description, Quantity: l.Quantity, UnitPrice: l.UnitPrice, Amount: amount, AccountID: l.AccountID}
	}
	return subtotal, lines
}

func (m *Module) Create(ctx context.Context, actor Actor, d Draft) (*model.CreditNote, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validateDraft(d); err != nil {
		return nil, err
	}
	subtotal, lines := calculated(d)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin credit note create: %w", err)
	}
	defer tx.Rollback()
	if err := requireDraftRefs(ctx, tx, d); err != nil {
		return nil, err
	}
	number, err := model.GenerateDocNumberContext(ctx, tx, "credit_notes", "cn_number", "CN")
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO credit_notes (cn_number,contact_id,invoice_id,cn_date,reason,status,subtotal,tax_amount,total,notes,created_by) VALUES (?,?,?,?,?,'draft',?,?,?,?,?)`, number, d.ContactID, d.InvoiceID, d.Date, d.Reason, subtotal, d.TaxAmount, subtotal+d.TaxAmount, d.Notes, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("insert credit note: %w", err)
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	id := int(id64)
	if err = insertLines(ctx, tx, id, lines); err != nil {
		return nil, err
	}
	created, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit credit note create: %w", err)
	}
	m.audit(ctx, actor, "credit_note.create", created, map[string]any{"after": snapshot(created)})
	return created, nil
}

func (m *Module) Update(ctx context.Context, actor Actor, id int, d Draft) (*model.CreditNote, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validateDraft(d); err != nil {
		return nil, err
	}
	subtotal, lines := calculated(d)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin credit note update: %w", err)
	}
	defer tx.Rollback()
	if err = lock(ctx, tx, id); err != nil {
		return nil, err
	}
	old, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if old.Status != model.StatusDraft {
		return nil, &ConflictError{Message: fmt.Sprintf("can only edit draft credit notes (current: %s)", old.Status)}
	}
	if err := requireDraftRefs(ctx, tx, d); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credit_notes SET contact_id=?,invoice_id=?,cn_date=?,reason=?,subtotal=?,tax_amount=?,total=?,notes=?,updated_at=datetime('now') WHERE id=?`, d.ContactID, d.InvoiceID, d.Date, d.Reason, subtotal, d.TaxAmount, subtotal+d.TaxAmount, d.Notes, id)
	if err != nil {
		return nil, fmt.Errorf("update credit note: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM credit_note_lines WHERE credit_note_id=?", id); err != nil {
		return nil, fmt.Errorf("delete credit note lines: %w", err)
	}
	if err = insertLines(ctx, tx, id, lines); err != nil {
		return nil, err
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit credit note update: %w", err)
	}
	if diff := audit.Diff(snapshot(old), snapshot(updated), []string{"contact_id", "invoice_id", "cn_date", "reason", "tax_amount", "notes", "total"}); diff != nil {
		m.audit(ctx, actor, "credit_note.update", updated, diff)
	}
	return updated, nil
}

func (m *Module) Delete(ctx context.Context, actor Actor, id int) (*model.CreditNote, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lock(ctx, tx, id); err != nil {
		return nil, err
	}
	old, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if old.Status != model.StatusDraft {
		return nil, &ConflictError{Message: fmt.Sprintf("can only delete draft credit notes (current: %s)", old.Status)}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM credit_notes WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("delete credit note: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit credit note delete: %w", err)
	}
	m.audit(ctx, actor, "credit_note.delete", old, map[string]any{"before": snapshot(old)})
	return old, nil
}

func (m *Module) Issue(ctx context.Context, actor Actor, id int) (*model.CreditNote, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin credit note issue: %w", err)
	}
	defer tx.Rollback()
	if err = lock(ctx, tx, id); err != nil {
		return nil, err
	}
	cn, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if cn.Status != model.StatusDraft {
		return nil, &ConflictError{Message: fmt.Sprintf("can only issue draft credit notes (current: %s)", cn.Status)}
	}
	ref, err := model.GenerateDocNumberContext(ctx, tx, "journal_entries", "reference", "JE")
	if err != nil {
		return nil, err
	}
	ar, err := accountByCode(ctx, tx, model.AccountCodeAR)
	if err != nil {
		return nil, err
	}
	if cn.InvoiceID != nil {
		if err = lockInvoice(ctx, tx, *cn.InvoiceID); err != nil {
			return nil, err
		}
		var contact, total, paid, credited, tax int
		var status string
		if err = tx.QueryRowContext(ctx, "SELECT contact_id,total,amount_paid,amount_credited,tax_amount,status FROM invoices WHERE id=?", *cn.InvoiceID).Scan(&contact, &total, &paid, &credited, &tax, &status); err != nil {
			return nil, fmt.Errorf("read invoice for credit: %w", err)
		}
		if contact != cn.ContactID {
			return nil, &ValidationError{Message: "credit note contact does not match invoice contact"}
		}
		var reversedTax int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(tax_amount),0) FROM credit_notes
			WHERE invoice_id=? AND status=? AND id<>?`, *cn.InvoiceID, model.StatusIssued, cn.ID).Scan(&reversedTax); err != nil {
			return nil, fmt.Errorf("read prior invoice tax credits: %w", err)
		}
		if reversedTax+cn.TaxAmount > tax {
			return nil, &ValidationError{Message: fmt.Sprintf("credit note tax (%d) exceeds remaining invoice tax (%d)", cn.TaxAmount, tax-reversedTax)}
		}
		if status == model.StatusDraft || status == model.StatusCancelled || status == model.StatusPaid {
			return nil, &ConflictError{Message: fmt.Sprintf("cannot apply credit to a %s invoice", status)}
		}
		if cn.Total > total-paid-credited {
			return nil, &ConflictError{Message: fmt.Sprintf("credit (%d) exceeds remaining balance (%d) on invoice", cn.Total, total-paid-credited)}
		}
	}
	lines := make([]journalLine, 0, len(cn.Lines)+2)
	for _, l := range cn.Lines {
		lines = append(lines, journalLine{l.AccountID, l.Amount, 0, l.Description})
	}
	if cn.TaxAmount > 0 {
		tax, err := accountByCode(ctx, tx, model.AccountCodeTax)
		if err != nil {
			return nil, err
		}
		lines = append(lines, journalLine{tax, cn.TaxAmount, 0, "Tax reversal"})
	}
	lines = append(lines, journalLine{ar, 0, cn.Total, cn.CNNumber})
	desc := fmt.Sprintf("Credit Note %s - %s", cn.CNNumber, cn.ContactName)
	if cn.InvoiceNumber != "" {
		desc = fmt.Sprintf("Credit Note %s for invoice %s", cn.CNNumber, cn.InvoiceNumber)
	}
	journalID, err := insertJournal(ctx, tx, ref, cn.CNDate, desc, id, actor.UserID, lines)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE credit_notes SET status=?,journal_id=?,updated_at=datetime('now') WHERE id=?", model.StatusIssued, journalID, id); err != nil {
		return nil, fmt.Errorf("issue credit note: %w", err)
	}
	if cn.InvoiceID != nil {
		if err = applyCredit(ctx, tx, *cn.InvoiceID, cn.Total); err != nil {
			return nil, err
		}
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit credit note issue: %w", err)
	}
	m.audit(ctx, actor, "credit_note.issue", updated, map[string]any{"after": map[string]any{"status": updated.Status}, "journal_id": updated.JournalID, "invoice_id": updated.InvoiceID})
	return updated, nil
}

func (m *Module) Void(ctx context.Context, actor Actor, id int) (*model.CreditNote, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin credit note void: %w", err)
	}
	defer tx.Rollback()
	if err = lock(ctx, tx, id); err != nil {
		return nil, err
	}
	cn, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if cn.Status != model.StatusIssued {
		return nil, &ConflictError{Message: fmt.Sprintf("can only void issued credit notes (current: %s)", cn.Status)}
	}
	ref, err := model.GenerateDocNumberContext(ctx, tx, "journal_entries", "reference", "JE")
	if err != nil {
		return nil, err
	}
	if cn.InvoiceID != nil {
		if err = lockInvoice(ctx, tx, *cn.InvoiceID); err != nil {
			return nil, err
		}
	}
	ar, err := accountByCode(ctx, tx, model.AccountCodeAR)
	if err != nil {
		return nil, err
	}
	lines := []journalLine{{ar, cn.Total, 0, "Void " + cn.CNNumber}}
	for _, l := range cn.Lines {
		lines = append(lines, journalLine{l.AccountID, 0, l.Amount, l.Description})
	}
	if cn.TaxAmount > 0 {
		tax, err := accountByCode(ctx, tx, model.AccountCodeTax)
		if err != nil {
			return nil, err
		}
		lines = append(lines, journalLine{tax, 0, cn.TaxAmount, "Tax"})
	}
	if _, err = insertJournal(ctx, tx, ref, cn.CNDate, "Void Credit Note "+cn.CNNumber, id, actor.UserID, lines); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE credit_notes SET status=?,updated_at=datetime('now') WHERE id=?", model.StatusVoid, id); err != nil {
		return nil, fmt.Errorf("void credit note: %w", err)
	}
	if cn.InvoiceID != nil {
		if err = unapplyCredit(ctx, tx, *cn.InvoiceID, cn.Total); err != nil {
			return nil, err
		}
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit credit note void: %w", err)
	}
	m.audit(ctx, actor, "credit_note.void", updated, map[string]any{"after": map[string]any{"status": updated.Status}, "invoice_id": updated.InvoiceID})
	return updated, nil
}

func insertLines(ctx context.Context, tx *sql.Tx, id int, lines []model.CreditNoteLine) error {
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_note_lines (credit_note_id,description,quantity,unit_price,amount,account_id) VALUES (?,?,?,?,?,?)`, id, l.Description, l.Quantity, l.UnitPrice, l.Amount, l.AccountID); err != nil {
			return fmt.Errorf("insert credit note line: %w", err)
		}
	}
	return nil
}
func lock(ctx context.Context, tx *sql.Tx, id int) error {
	res, err := tx.ExecContext(ctx, "UPDATE credit_notes SET id=id WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("lock credit note: %w", err)
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
func lockInvoice(ctx context.Context, tx *sql.Tx, id int) error {
	res, err := tx.ExecContext(ctx, "UPDATE invoices SET id=id WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return &ValidationError{Message: "linked invoice not found"}
	}
	return nil
}

type journalLine struct {
	accountID, debit, credit int
	memo                     string
}

func insertJournal(ctx context.Context, tx *sql.Tx, ref, date, desc string, sourceID, userID int, lines []journalLine) (int, error) {
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
	res, err := tx.ExecContext(ctx, `INSERT INTO journal_entries (entry_date,reference,description,source_type,source_id,is_posted,created_by) VALUES (?,?,?,?,?,1,?)`, date, ref, desc, model.SourceCreditNote, sourceID, userID)
	if err != nil {
		return 0, fmt.Errorf("insert credit note journal: %w", err)
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	id := int(id64)
	for _, l := range lines {
		if _, err = tx.ExecContext(ctx, `INSERT INTO journal_lines (entry_id,account_id,debit,credit,memo) VALUES (?,?,?,?,?)`, id, l.accountID, l.debit, l.credit, l.memo); err != nil {
			return 0, fmt.Errorf("insert credit note journal line: %w", err)
		}
	}
	return id, nil
}
func accountByCode(ctx context.Context, q queryer, code string) (int, error) {
	var id int
	err := q.QueryRowContext(ctx, "SELECT id FROM accounts WHERE code=?", code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("account %s not found", code)
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func requireDraftRefs(ctx context.Context, q queryer, d Draft) error {
	fields := map[string]string{}
	var contactType string
	if err := q.QueryRowContext(ctx, "SELECT contact_type FROM contacts WHERE id=? AND is_active=1", d.ContactID).Scan(&contactType); err != nil || contactType != "customer" {
		fields["contact_id"] = "active customer not found"
	}
	if d.InvoiceID != nil {
		var exists int
		if err := q.QueryRowContext(ctx, "SELECT 1 FROM invoices WHERE id=?", *d.InvoiceID).Scan(&exists); err != nil {
			fields["invoice_id"] = "invoice not found"
		}
	}
	for i, line := range d.Lines {
		var accountType string
		if err := q.QueryRowContext(ctx, "SELECT account_type FROM accounts WHERE id=? AND is_active=1", line.AccountID).Scan(&accountType); err != nil || accountType != model.AccountTypeRevenue {
			fields["lines["+strconv.Itoa(i)+"].account_id"] = "active revenue account not found"
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}
func applyCredit(ctx context.Context, tx *sql.Tx, id, amount int) error {
	var total, paid, credited int
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT total,amount_paid,amount_credited,status FROM invoices WHERE id=?", id).Scan(&total, &paid, &credited, &status); err != nil {
		return fmt.Errorf("read invoice for credit: %w", err)
	}
	next := credited + amount
	if next > total-paid {
		return &ConflictError{Message: fmt.Sprintf("credit (%d) exceeds remaining balance (%d) on invoice", amount, total-paid)}
	}
	newStatus := status
	if paid+next >= total {
		if paid == 0 {
			newStatus = model.StatusCancelled
		} else {
			newStatus = model.StatusPaid
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE invoices SET amount_credited=?,status=?,updated_at=datetime('now') WHERE id=?", next, newStatus, id)
	return err
}
func unapplyCredit(ctx context.Context, tx *sql.Tx, id, amount int) error {
	var total, paid, credited int
	if err := tx.QueryRowContext(ctx, "SELECT total,amount_paid,amount_credited FROM invoices WHERE id=?", id).Scan(&total, &paid, &credited); err != nil {
		return fmt.Errorf("read invoice for void: %w", err)
	}
	next := credited - amount
	if next < 0 {
		next = 0
	}
	status := model.StatusSent
	if paid+next >= total {
		if paid == 0 {
			status = model.StatusCancelled
		} else {
			status = model.StatusPaid
		}
	} else if paid > 0 {
		status = model.StatusPartial
	}
	_, err := tx.ExecContext(ctx, "UPDATE invoices SET amount_credited=?,status=?,updated_at=datetime('now') WHERE id=?", next, status, id)
	return err
}
func snapshot(cn *model.CreditNote) map[string]any {
	return map[string]any{"contact_id": cn.ContactID, "invoice_id": cn.InvoiceID, "cn_date": cn.CNDate, "reason": cn.Reason, "tax_amount": cn.TaxAmount, "notes": cn.Notes, "status": cn.Status, "total": cn.Total, "line_count": len(cn.Lines)}
}
func (m *Module) audit(ctx context.Context, actor Actor, action string, cn *model.CreditNote, metadata map[string]any) {
	audit.Log(ctx, m.db, audit.Event{Action: action, TargetType: "credit_note", TargetID: int64(cn.ID), TargetLabel: cn.CNNumber, Metadata: metadata, ActorID: int64(actor.UserID)})
}
