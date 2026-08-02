package invoice

import (
	"context"
	"fmt"

	"github.com/naufal/latasya-erp/internal/model"
)

type Filter struct {
	Status string
	Search string
	Limit  int
	Offset int
}

type ListResult struct {
	Invoices []model.Invoice
	Total    int
}

type Detail struct {
	Invoice     *model.Invoice
	CreditNotes []model.CreditNote
}

type View struct {
	Detail
	AssetAccounts []model.Account
}

type FormOptions struct {
	Contacts                     []model.Contact
	RevenueAccounts              []model.Account
	DefaultRevenueAccountID      int
	RecurringDescriptionTemplate string
}

type Document struct {
	Invoice *model.Invoice
	Company *model.CompanyProfile
}

func (m *Module) List(ctx context.Context, filter Filter) (*ListResult, error) {
	total, err := m.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	invoices, err := m.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	return &ListResult{Invoices: invoices, Total: total}, nil
}

func (m *Module) Count(ctx context.Context, filter Filter) (int, error) {
	where, args := invoiceWhere(filter)
	var total int
	if err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices i JOIN contacts c ON c.id=i.contact_id WHERE 1=1`+where, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count invoices: %w", err)
	}
	return total, nil
}

func (m *Module) Find(ctx context.Context, filter Filter) ([]model.Invoice, error) {
	where, args := invoiceWhere(filter)
	query := `SELECT i.id, i.invoice_number, i.contact_id, i.invoice_date, i.due_date, i.status,
		i.subtotal, i.tax_amount, i.total, i.amount_paid, i.amount_credited, COALESCE(i.notes,''),
		i.journal_id, i.created_by, i.created_at, i.updated_at, c.name,
		COALESCE((SELECT MAX(payment_date) FROM payments WHERE payment_type='invoice' AND reference_id=i.id), i.updated_at)
		FROM invoices i JOIN contacts c ON c.id=i.contact_id WHERE 1=1` + where + ` ORDER BY i.invoice_date DESC, i.id DESC`
	listArgs := append([]any(nil), args...)
	if filter.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		listArgs = append(listArgs, filter.Limit, filter.Offset)
	}
	rows, err := m.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("find invoices: %w", err)
	}
	defer rows.Close()
	invoices := []model.Invoice{}
	for rows.Next() {
		var inv model.Invoice
		if err := rows.Scan(&inv.ID, &inv.InvoiceNumber, &inv.ContactID, &inv.InvoiceDate, &inv.DueDate, &inv.Status,
			&inv.Subtotal, &inv.TaxAmount, &inv.Total, &inv.AmountPaid, &inv.AmountCredited, &inv.Notes,
			&inv.JournalID, &inv.CreatedBy, &inv.CreatedAt, &inv.UpdatedAt, &inv.ContactName, &inv.PaidDate); err != nil {
			return nil, fmt.Errorf("scan invoice: %w", err)
		}
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invoices: %w", err)
	}
	return invoices, nil
}

func (m *Module) Get(ctx context.Context, id int) (*model.Invoice, error) {
	return m.get(ctx, id)
}

func (m *Module) Detail(ctx context.Context, id int) (*Detail, error) {
	inv, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	notes, err := model.ListCreditNotesForInvoiceContext(ctx, m.db, id)
	if err != nil {
		return nil, fmt.Errorf("list invoice credit notes: %w", err)
	}
	if notes == nil {
		notes = []model.CreditNote{}
	}
	return &Detail{Invoice: inv, CreditNotes: notes}, nil
}

func (m *Module) View(ctx context.Context, id int) (*View, error) {
	detail, err := m.Detail(ctx, id)
	if err != nil {
		return nil, err
	}
	active := true
	accounts, err := model.ListAccountsContext(ctx, m.db, model.AccountFilter{Type: "asset", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list payment accounts: %w", err)
	}
	return &View{Detail: *detail, AssetAccounts: accounts}, nil
}

func (m *Module) FormOptions(ctx context.Context) (*FormOptions, error) {
	active := true
	contacts, err := model.ListContactsContext(ctx, m.db, model.ContactFilter{Type: "customer", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list invoice contacts: %w", err)
	}
	revenue, err := model.ListAccountsContext(ctx, m.db, model.AccountFilter{Type: "revenue", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list revenue accounts: %w", err)
	}
	profile, err := model.GetCompanyProfileContext(ctx, m.db)
	if err != nil {
		return nil, fmt.Errorf("load invoice defaults: %w", err)
	}
	return &FormOptions{Contacts: contacts, RevenueAccounts: revenue,
		DefaultRevenueAccountID:      profile.DefaultRevenueAccountID,
		RecurringDescriptionTemplate: profile.RecurringDescriptionTemplate}, nil
}

func (m *Module) Document(ctx context.Context, id int) (*Document, error) {
	inv, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	company, err := model.GetCompanyProfileContext(ctx, m.db)
	if err != nil {
		return nil, fmt.Errorf("load invoice company profile: %w", err)
	}
	return &Document{Invoice: inv, Company: company}, nil
}

func (m *Module) RevenueAccounts(ctx context.Context) ([]model.Account, error) {
	active := true
	return model.ListAccountsContext(ctx, m.db, model.AccountFilter{Type: "revenue", IsActive: &active})
}

func invoiceWhere(filter Filter) (string, []any) {
	where := ""
	args := []any{}
	if filter.Status != "" {
		where += " AND i.status=?"
		args = append(args, filter.Status)
	}
	if filter.Search != "" {
		pattern := "%" + filter.Search + "%"
		where += " AND (i.invoice_number LIKE ? OR c.name LIKE ?)"
		args = append(args, pattern, pattern)
	}
	return where, args
}
