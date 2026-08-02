package invoice

import (
	"context"
	"fmt"
	"strings"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/contact"
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

type ShareInfo struct {
	InvoiceNumber string
	ContactName   string
	Phone         string
	PortalCode    string
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
	notes, err := m.creditNotes.ForInvoice(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list invoice credit notes: %w", err)
	}
	return &Detail{Invoice: inv, CreditNotes: notes}, nil
}

func (m *Module) View(ctx context.Context, id int) (*View, error) {
	detail, err := m.Detail(ctx, id)
	if err != nil {
		return nil, err
	}
	active := true
	result, err := m.accounts.List(ctx, account.Filter{Type: "asset", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list payment accounts: %w", err)
	}
	return &View{Detail: *detail, AssetAccounts: result.Accounts}, nil
}

func (m *Module) FormOptions(ctx context.Context) (*FormOptions, error) {
	active := true
	contacts, err := m.contacts.List(ctx, contact.Filter{Type: "customer", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list invoice contacts: %w", err)
	}
	revenue, err := m.accounts.List(ctx, account.Filter{Type: "revenue", IsActive: &active})
	if err != nil {
		return nil, fmt.Errorf("list revenue accounts: %w", err)
	}
	profile, err := m.company.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load invoice defaults: %w", err)
	}
	return &FormOptions{Contacts: contacts.Contacts, RevenueAccounts: revenue.Accounts,
		DefaultRevenueAccountID:      profile.DefaultRevenueAccountID,
		RecurringDescriptionTemplate: profile.RecurringDescriptionTemplate}, nil
}

func (m *Module) Document(ctx context.Context, id int) (*Document, error) {
	inv, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	company, err := m.company.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("load invoice company profile: %w", err)
	}
	return &Document{Invoice: inv, Company: company}, nil
}

func (m *Module) RevenueAccounts(ctx context.Context) ([]model.Account, error) {
	active := true
	result, err := m.accounts.List(ctx, account.Filter{Type: "revenue", IsActive: &active})
	if err != nil {
		return nil, err
	}
	return result.Accounts, nil
}

// PortalInvoices returns finalized invoices visible to a portal family.
func (m *Module) PortalInvoices(ctx context.Context, contactIDs []int) ([]model.Invoice, error) {
	if len(contactIDs) == 0 {
		return []model.Invoice{}, nil
	}
	placeholders := make([]string, len(contactIDs))
	args := make([]any, len(contactIDs))
	for i, id := range contactIDs {
		placeholders[i], args[i] = "?", id
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, invoice_number, contact_id, invoice_date, due_date, status,
			subtotal, tax_amount, total, amount_paid, amount_credited, COALESCE(notes,''),
			COALESCE((SELECT MAX(payment_date) FROM payments WHERE payment_type='invoice' AND reference_id=invoices.id), updated_at)
		FROM invoices
		WHERE contact_id IN (`+strings.Join(placeholders, ",")+`) AND status != ?
		ORDER BY invoice_date DESC, id DESC`, append(args, model.StatusDraft)...)
	if err != nil {
		return nil, fmt.Errorf("list portal invoices: %w", err)
	}
	defer rows.Close()
	invoices := []model.Invoice{}
	for rows.Next() {
		var inv model.Invoice
		if err := rows.Scan(&inv.ID, &inv.InvoiceNumber, &inv.ContactID, &inv.InvoiceDate, &inv.DueDate, &inv.Status,
			&inv.Subtotal, &inv.TaxAmount, &inv.Total, &inv.AmountPaid, &inv.AmountCredited, &inv.Notes, &inv.PaidDate); err != nil {
			return nil, fmt.Errorf("scan portal invoice: %w", err)
		}
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate portal invoices: %w", err)
	}
	return invoices, nil
}

func (m *Module) PrepareShare(ctx context.Context, actor Actor, id int) (*ShareInfo, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	inv, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if inv.Status == model.StatusDraft {
		return nil, &ConflictError{Message: "draft invoice is not shareable"}
	}
	invoiceContact, err := m.contacts.Get(ctx, inv.ContactID)
	if err != nil {
		return nil, fmt.Errorf("load invoice contact: %w", err)
	}
	info := &ShareInfo{InvoiceNumber: inv.InvoiceNumber, ContactName: invoiceContact.Name, Phone: invoiceContact.Phone}
	if invoiceContact.Phone == "" {
		return info, nil
	}
	info.PortalCode, err = m.contacts.EnsurePortalCode(ctx, contact.PortalIssuer{UserID: actor.UserID, CanIssue: actor.CanManage}, invoiceContact.ID)
	if err != nil {
		return nil, fmt.Errorf("load portal code: %w", err)
	}
	return info, nil
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
