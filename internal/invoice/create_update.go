package invoice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/documentnumber"
	"github.com/naufal/latasya-erp/internal/model"
)

func (m *Module) Create(ctx context.Context, actor Actor, draft Draft) (*model.Invoice, error) {
	return m.create(ctx, actor, draft, true, "")
}

var errRecurringAlreadyExists = errors.New("customer already invoiced for month")

func (m *Module) create(ctx context.Context, actor Actor, draft Draft, logAudit bool, recurringMonth string) (*model.Invoice, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validateDraft(draft); err != nil {
		return nil, err
	}

	subtotal, lines := calculateLines(draft.Lines)

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin invoice create: %w", err)
	}
	defer tx.Rollback()
	if recurringMonth != "" {
		claim, err := tx.ExecContext(ctx, `
			INSERT INTO invoice_recurring_claims (contact_id, invoice_month)
			SELECT ?, ?
			WHERE NOT EXISTS (
				SELECT 1 FROM invoices WHERE contact_id=? AND substr(invoice_date, 1, 7)=?
			)
			ON CONFLICT(contact_id, invoice_month) DO NOTHING`,
			draft.ContactID, recurringMonth, draft.ContactID, recurringMonth)
		if err != nil {
			return nil, fmt.Errorf("claim recurring invoice: %w", err)
		}
		claimed, err := claim.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("recurring claim rows: %w", err)
		}
		if claimed == 0 {
			return nil, errRecurringAlreadyExists
		}
	}

	number, err := documentnumber.NextAt(ctx, tx, documentnumber.Invoice, m.now())
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO invoices (
			invoice_number, contact_id, invoice_date, due_date, status,
			subtotal, tax_amount, total, amount_paid, notes, created_by
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		number, draft.ContactID, draft.InvoiceDate, draft.DueDate, model.StatusDraft,
		subtotal, draft.TaxAmount, subtotal+draft.TaxAmount, draft.Notes, actor.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert invoice: %w", err)
	}
	id64, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("invoice id: %w", err)
	}
	id := int(id64)
	if recurringMonth != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE invoice_recurring_claims SET invoice_id=?
			WHERE contact_id=? AND invoice_month=?`, id, draft.ContactID, recurringMonth); err != nil {
			return nil, fmt.Errorf("link recurring invoice claim: %w", err)
		}
	}

	if err := insertLines(ctx, tx, id, lines); err != nil {
		return nil, err
	}
	created, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("load created invoice: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit invoice create: %w", err)
	}

	if logAudit {
		audit.Log(ctx, m.db, audit.Event{
			Action:      "invoice.create",
			TargetType:  "invoice",
			TargetID:    int64(created.ID),
			TargetLabel: created.InvoiceNumber,
			ActorID:     int64(actor.UserID),
			Metadata: map[string]any{"after": map[string]any{
				"contact_id":   created.ContactID,
				"invoice_date": created.InvoiceDate,
				"due_date":     created.DueDate,
				"tax_amount":   created.TaxAmount,
				"total":        created.Total,
				"line_count":   len(created.Lines),
			}},
		})
	}
	return created, nil
}

func (m *Module) Update(ctx context.Context, actor Actor, id int, draft Draft) (*model.Invoice, error) {
	if err := m.CheckEditable(ctx, actor, id); err != nil {
		return nil, err
	}
	if err := validateDraft(draft); err != nil {
		return nil, err
	}

	subtotal, lines := calculateLines(draft.Lines)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin invoice update: %w", err)
	}
	defer tx.Rollback()

	// Acquire SQLite's write lock before taking the audit snapshot so two
	// updates cannot both observe the same before-state.
	locked, err := tx.ExecContext(ctx, "UPDATE invoices SET id=id WHERE id=? AND status=?", id, model.StatusDraft)
	if err != nil {
		return nil, fmt.Errorf("lock invoice update: %w", err)
	}
	lockedRows, err := locked.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("locked invoice rows: %w", err)
	}
	if lockedRows == 0 {
		return nil, &ConflictError{Message: "invoice is no longer editable"}
	}
	existing, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("load invoice before update: %w", err)
	}
	var claimContactID int
	var claimMonth string
	claimErr := tx.QueryRowContext(ctx, `
		SELECT contact_id, invoice_month FROM invoice_recurring_claims WHERE invoice_id=?`, id,
	).Scan(&claimContactID, &claimMonth)
	if claimErr != nil && !errors.Is(claimErr, sql.ErrNoRows) {
		return nil, fmt.Errorf("load recurring invoice claim: %w", claimErr)
	}
	newMonth := draft.InvoiceDate
	if len(newMonth) >= 7 {
		newMonth = newMonth[:7]
	}
	if claimErr == nil && (claimContactID != draft.ContactID || claimMonth != newMonth) {
		if _, err := tx.ExecContext(ctx, `
			UPDATE invoice_recurring_claims SET contact_id=?, invoice_month=?
			WHERE invoice_id=?`, draft.ContactID, newMonth, id); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return nil, &ConflictError{Message: "customer already has a recurring invoice for that month"}
			}
			return nil, fmt.Errorf("move recurring invoice claim: %w", err)
		}
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE invoices
		SET contact_id=?, invoice_date=?, due_date=?, subtotal=?, tax_amount=?, total=?,
			notes=?, updated_at=datetime('now')
		WHERE id=? AND status=?`,
		draft.ContactID, draft.InvoiceDate, draft.DueDate, subtotal, draft.TaxAmount,
		subtotal+draft.TaxAmount, draft.Notes, id, model.StatusDraft,
	)
	if err != nil {
		return nil, fmt.Errorf("update invoice: %w", err)
	}
	updatedRows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("updated invoice rows: %w", err)
	}
	if updatedRows == 0 {
		return nil, &ConflictError{Message: "invoice is no longer editable"}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM invoice_lines WHERE invoice_id = ?", id); err != nil {
		return nil, fmt.Errorf("delete invoice lines: %w", err)
	}
	if err := insertLines(ctx, tx, id, lines); err != nil {
		return nil, err
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("load updated invoice: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit invoice update: %w", err)
	}

	metadata := audit.Diff(
		invoiceAuditFields(existing), invoiceAuditFields(updated),
		[]string{"contact_id", "invoice_date", "due_date", "tax_amount", "notes", "total"},
	)
	if metadata != nil {
		audit.Log(ctx, m.db, audit.Event{
			Action:      "invoice.update",
			TargetType:  "invoice",
			TargetID:    int64(id),
			TargetLabel: updated.InvoiceNumber,
			ActorID:     int64(actor.UserID),
			Metadata:    metadata,
		})
	}
	return updated, nil
}

// CheckEditable preserves update precondition ordering for transports that must
// reject a missing or non-draft invoice before decoding the request body.
func (m *Module) CheckEditable(ctx context.Context, actor Actor, id int) error {
	if err := requireManager(actor); err != nil {
		return err
	}
	var status string
	err := m.db.QueryRowContext(ctx, "SELECT status FROM invoices WHERE id=?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check editable invoice: %w", err)
	}
	if status != model.StatusDraft {
		return &ConflictError{Message: fmt.Sprintf("can only edit draft invoices (current: %s)", status)}
	}
	return nil
}

func (m *Module) get(ctx context.Context, id int) (*model.Invoice, error) {
	return getWith(ctx, m.db, id)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getWith(ctx context.Context, db queryer, id int) (*model.Invoice, error) {
	inv := &model.Invoice{}
	err := db.QueryRowContext(ctx, `
		SELECT i.id, i.invoice_number, i.contact_id, i.invoice_date, i.due_date, i.status,
			i.subtotal, i.tax_amount, i.total, i.amount_paid, i.amount_credited, COALESCE(i.notes,''),
			i.journal_id, i.created_by, i.created_at, i.updated_at, c.name
		FROM invoices i JOIN contacts c ON c.id = i.contact_id WHERE i.id = ?`, id,
	).Scan(&inv.ID, &inv.InvoiceNumber, &inv.ContactID, &inv.InvoiceDate, &inv.DueDate, &inv.Status,
		&inv.Subtotal, &inv.TaxAmount, &inv.Total, &inv.AmountPaid, &inv.AmountCredited, &inv.Notes,
		&inv.JournalID, &inv.CreatedBy, &inv.CreatedAt, &inv.UpdatedAt, &inv.ContactName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get invoice: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT il.id, il.invoice_id, il.description, il.quantity, il.unit_price, il.amount,
			il.account_id, a.code, a.name
		FROM invoice_lines il JOIN accounts a ON a.id = il.account_id
		WHERE il.invoice_id = ? ORDER BY il.id`, id)
	if err != nil {
		return nil, fmt.Errorf("get invoice lines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line model.InvoiceLine
		if err := rows.Scan(&line.ID, &line.InvoiceID, &line.Description, &line.Quantity,
			&line.UnitPrice, &line.Amount, &line.AccountID, &line.AccountCode, &line.AccountName); err != nil {
			return nil, fmt.Errorf("scan invoice line: %w", err)
		}
		inv.Lines = append(inv.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invoice lines: %w", err)
	}
	return inv, nil
}

func calculateLines(input []DraftLine) (int, []model.InvoiceLine) {
	lines := make([]model.InvoiceLine, len(input))
	var subtotal int
	for i, line := range input {
		lines[i] = model.InvoiceLine{
			Description: line.Description,
			Quantity:    line.Quantity,
			UnitPrice:   line.UnitPrice,
			AccountID:   line.AccountID,
		}
		lines[i].Amount = line.Quantity * line.UnitPrice / 100
		subtotal += lines[i].Amount
	}
	return subtotal, lines
}

func insertLines(ctx context.Context, tx *sql.Tx, invoiceID int, lines []model.InvoiceLine) error {
	for _, line := range lines {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO invoice_lines (invoice_id, description, quantity, unit_price, amount, account_id)
			VALUES (?, ?, ?, ?, ?, ?)`,
			invoiceID, line.Description, line.Quantity, line.UnitPrice, line.Amount, line.AccountID,
		)
		if err != nil {
			return fmt.Errorf("insert invoice line: %w", err)
		}
	}
	return nil
}

func invoiceAuditFields(inv *model.Invoice) map[string]any {
	return map[string]any{
		"contact_id":   inv.ContactID,
		"invoice_date": inv.InvoiceDate,
		"due_date":     inv.DueDate,
		"tax_amount":   inv.TaxAmount,
		"notes":        inv.Notes,
		"total":        inv.Total,
	}
}
