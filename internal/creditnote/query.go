package creditnote

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/model"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type scanner interface{ Scan(...any) error }

func where(f Filter) (string, []any) {
	var clause string
	var args []any
	if f.Status != "" {
		clause += " AND cn.status=?"
		args = append(args, f.Status)
	}
	if f.Search != "" {
		clause += " AND (cn.cn_number LIKE ? OR c.name LIKE ? OR i.invoice_number LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s, s)
	}
	return clause, args
}

func (m *Module) List(ctx context.Context, f Filter) (*ListResult, error) {
	clause, args := where(f)
	var total int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_notes cn JOIN contacts c ON c.id=cn.contact_id LEFT JOIN invoices i ON i.id=cn.invoice_id WHERE 1=1`+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count credit notes: %w", err)
	}
	query := `SELECT cn.id,cn.cn_number,cn.contact_id,cn.invoice_id,cn.cn_date,cn.reason,cn.status,cn.subtotal,cn.tax_amount,cn.total,COALESCE(cn.notes,''),cn.journal_id,cn.created_by,cn.created_at,cn.updated_at,c.name,COALESCE(i.invoice_number,'') FROM credit_notes cn JOIN contacts c ON c.id=cn.contact_id LEFT JOIN invoices i ON i.id=cn.invoice_id WHERE 1=1` + clause + ` ORDER BY cn.cn_date DESC,cn.id DESC`
	listArgs := append([]any(nil), args...)
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		listArgs = append(listArgs, f.Limit, f.Offset)
	}
	rows, err := m.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("list credit notes: %w", err)
	}
	defer rows.Close()
	notes := []model.CreditNote{}
	for rows.Next() {
		var cn model.CreditNote
		if err := scanCreditNote(rows, &cn); err != nil {
			return nil, fmt.Errorf("scan credit note: %w", err)
		}
		cn.CreatedAt, cn.UpdatedAt = instant(cn.CreatedAt), instant(cn.UpdatedAt)
		notes = append(notes, cn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate credit notes: %w", err)
	}
	return &ListResult{CreditNotes: notes, Total: total}, nil
}

func scanCreditNote(row scanner, cn *model.CreditNote) error {
	return row.Scan(&cn.ID, &cn.CNNumber, &cn.ContactID, &cn.InvoiceID, &cn.CNDate, &cn.Reason, &cn.Status, &cn.Subtotal, &cn.TaxAmount, &cn.Total, &cn.Notes, &cn.JournalID, &cn.CreatedBy, &cn.CreatedAt, &cn.UpdatedAt, &cn.ContactName, &cn.InvoiceNumber)
}
func getWith(ctx context.Context, q queryer, id int) (*model.CreditNote, error) {
	cn := &model.CreditNote{}
	err := scanCreditNote(q.QueryRowContext(ctx, `SELECT cn.id,cn.cn_number,cn.contact_id,cn.invoice_id,cn.cn_date,cn.reason,cn.status,cn.subtotal,cn.tax_amount,cn.total,COALESCE(cn.notes,''),cn.journal_id,cn.created_by,cn.created_at,cn.updated_at,c.name,COALESCE(i.invoice_number,'') FROM credit_notes cn JOIN contacts c ON c.id=cn.contact_id LEFT JOIN invoices i ON i.id=cn.invoice_id WHERE cn.id=?`, id), cn)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get credit note: %w", err)
	}
	cn.CreatedAt, cn.UpdatedAt = instant(cn.CreatedAt), instant(cn.UpdatedAt)
	rows, err := q.QueryContext(ctx, `SELECT cnl.id,cnl.credit_note_id,cnl.description,cnl.quantity,cnl.unit_price,cnl.amount,cnl.account_id,a.code,a.name FROM credit_note_lines cnl JOIN accounts a ON a.id=cnl.account_id WHERE cnl.credit_note_id=? ORDER BY cnl.id`, id)
	if err != nil {
		return nil, fmt.Errorf("get credit note lines: %w", err)
	}
	defer rows.Close()
	cn.Lines = []model.CreditNoteLine{}
	for rows.Next() {
		var l model.CreditNoteLine
		if err := rows.Scan(&l.ID, &l.CreditNoteID, &l.Description, &l.Quantity, &l.UnitPrice, &l.Amount, &l.AccountID, &l.AccountCode, &l.AccountName); err != nil {
			return nil, fmt.Errorf("scan credit note line: %w", err)
		}
		cn.Lines = append(cn.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate credit note lines: %w", err)
	}
	return cn, nil
}
func (m *Module) Get(ctx context.Context, id int) (*model.CreditNote, error) {
	return getWith(ctx, m.db, id)
}

func (m *Module) ForInvoice(ctx context.Context, invoiceID int) ([]model.CreditNote, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id,cn_number,cn_date,reason,status,total,journal_id FROM credit_notes WHERE invoice_id=? ORDER BY cn_date DESC,id DESC`, invoiceID)
	if err != nil {
		return nil, fmt.Errorf("list invoice credit notes: %w", err)
	}
	defer rows.Close()
	notes := []model.CreditNote{}
	for rows.Next() {
		var cn model.CreditNote
		if err := rows.Scan(&cn.ID, &cn.CNNumber, &cn.CNDate, &cn.Reason, &cn.Status, &cn.Total, &cn.JournalID); err != nil {
			return nil, fmt.Errorf("scan invoice credit note: %w", err)
		}
		notes = append(notes, cn)
	}
	return notes, rows.Err()
}

func (m *Module) Options(ctx context.Context) (*FormOptions, error) {
	active := true
	contacts, err := m.contacts.List(ctx, contact.Filter{Type: "customer", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list credit note customers: %w", err)
	}
	accounts, err := m.accounts.List(ctx, account.Filter{Type: model.AccountTypeRevenue, IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list credit note revenue accounts: %w", err)
	}
	return &FormOptions{Contacts: contacts.Contacts, RevenueAccounts: accounts.Accounts}, nil
}
func instant(value string) string {
	parsed, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return value
	}
	return parsed.UTC().Format(time.RFC3339)
}
