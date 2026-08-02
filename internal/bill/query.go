package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func where(f Filter) (string, []any) {
	var clause string
	var args []any
	if f.Status != "" {
		clause += " AND b.status=?"
		args = append(args, f.Status)
	}
	if f.Search != "" {
		clause += " AND (b.bill_number LIKE ? OR c.name LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s)
	}
	return clause, args
}

func (m *Module) List(ctx context.Context, f Filter) (*ListResult, error) {
	clause, args := where(f)
	var total int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bills b JOIN contacts c ON c.id=b.contact_id WHERE 1=1`+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count bills: %w", err)
	}
	query := `SELECT b.id,b.bill_number,b.contact_id,b.bill_date,b.due_date,b.status,b.subtotal,b.tax_amount,b.total,b.amount_paid,
		COALESCE(b.notes,''),b.journal_id,b.created_by,b.created_at,b.updated_at,c.name
		FROM bills b JOIN contacts c ON c.id=b.contact_id WHERE 1=1` + clause + ` ORDER BY b.bill_date DESC,b.id DESC`
	listArgs := append([]any(nil), args...)
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		listArgs = append(listArgs, f.Limit, f.Offset)
	}
	rows, err := m.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("list bills: %w", err)
	}
	defer rows.Close()
	bills := []model.Bill{}
	for rows.Next() {
		var b model.Bill
		if err := scanBill(rows, &b); err != nil {
			return nil, fmt.Errorf("scan bill: %w", err)
		}
		b.CreatedAt, b.UpdatedAt = instant(b.CreatedAt), instant(b.UpdatedAt)
		bills = append(bills, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bills: %w", err)
	}
	return &ListResult{Bills: bills, Total: total}, nil
}

type scanner interface{ Scan(...any) error }

func scanBill(row scanner, b *model.Bill) error {
	return row.Scan(&b.ID, &b.BillNumber, &b.ContactID, &b.BillDate, &b.DueDate, &b.Status, &b.Subtotal, &b.TaxAmount, &b.Total, &b.AmountPaid,
		&b.Notes, &b.JournalID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.ContactName)
}

func getWith(ctx context.Context, q queryer, id int) (*model.Bill, error) {
	b := &model.Bill{}
	err := scanBill(q.QueryRowContext(ctx, `SELECT b.id,b.bill_number,b.contact_id,b.bill_date,b.due_date,b.status,b.subtotal,b.tax_amount,b.total,b.amount_paid,
		COALESCE(b.notes,''),b.journal_id,b.created_by,b.created_at,b.updated_at,c.name
		FROM bills b JOIN contacts c ON c.id=b.contact_id WHERE b.id=?`, id), b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get bill: %w", err)
	}
	b.CreatedAt, b.UpdatedAt = instant(b.CreatedAt), instant(b.UpdatedAt)
	rows, err := q.QueryContext(ctx, `SELECT bl.id,bl.bill_id,bl.description,bl.quantity,bl.unit_price,bl.amount,bl.account_id,a.code,a.name
		FROM bill_lines bl JOIN accounts a ON a.id=bl.account_id WHERE bl.bill_id=? ORDER BY bl.id`, id)
	if err != nil {
		return nil, fmt.Errorf("get bill lines: %w", err)
	}
	defer rows.Close()
	b.Lines = []model.BillLine{}
	for rows.Next() {
		var l model.BillLine
		if err := rows.Scan(&l.ID, &l.BillID, &l.Description, &l.Quantity, &l.UnitPrice, &l.Amount, &l.AccountID, &l.AccountCode, &l.AccountName); err != nil {
			return nil, fmt.Errorf("scan bill line: %w", err)
		}
		b.Lines = append(b.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bill lines: %w", err)
	}
	return b, nil
}

func (m *Module) Get(ctx context.Context, id int) (*model.Bill, error) { return getWith(ctx, m.db, id) }

func (m *Module) Options(ctx context.Context) (*FormOptions, error) {
	active := true
	contacts, err := model.ListContactsContext(ctx, m.db, model.ContactFilter{Type: "supplier", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list bill suppliers: %w", err)
	}
	expense, err := model.ListAccountsContext(ctx, m.db, model.AccountFilter{Type: model.AccountTypeExpense, IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list bill expense accounts: %w", err)
	}
	assets, err := model.ListAccountsContext(ctx, m.db, model.AccountFilter{Type: model.AccountTypeAsset, IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list bill payment accounts: %w", err)
	}
	return &FormOptions{Contacts: contacts, ExpenseAccounts: expense, AssetAccounts: assets}, nil
}

func instant(value string) string {
	parsed, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return value
	}
	return parsed.UTC().Format(time.RFC3339)
}
