package company

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getWith(ctx context.Context, q queryer) (*model.CompanyProfile, error) {
	c := &model.CompanyProfile{}
	var accountID *int
	err := q.QueryRowContext(ctx, `SELECT name,tagline,address,phone,email,npwp,bank_name,bank_account_number,bank_account_holder,invoice_footer,default_revenue_account_id,recurring_description_template,updated_at FROM company_profile WHERE id=1`).Scan(&c.Name, &c.Tagline, &c.Address, &c.Phone, &c.Email, &c.NPWP, &c.BankName, &c.BankAccountNumber, &c.BankAccountHolder, &c.InvoiceFooter, &accountID, &c.RecurringDescriptionTemplate, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get company profile: %w", err)
	}
	if accountID != nil {
		c.DefaultRevenueAccountID = *accountID
	}
	return c, nil
}

func (m *Module) Get(ctx context.Context) (*model.CompanyProfile, error) { return getWith(ctx, m.db) }

func validate(c model.CompanyProfile) error {
	fields := map[string]string{}
	if strings.TrimSpace(c.Name) == "" {
		fields["name"] = "required"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func (m *Module) Update(ctx context.Context, actor Actor, c model.CompanyProfile) (*model.CompanyProfile, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validate(c); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin company profile update: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE company_profile SET id=id WHERE id=1"); err != nil {
		return nil, fmt.Errorf("lock company profile: %w", err)
	}
	old, err := getWith(ctx, tx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if c.DefaultRevenueAccountID != 0 {
		var accountType string
		var active bool
		err := tx.QueryRowContext(ctx, "SELECT account_type,is_active FROM accounts WHERE id=?", c.DefaultRevenueAccountID).Scan(&accountType, &active)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (accountType != model.AccountTypeRevenue || !active) {
			return nil, &ValidationError{Fields: map[string]string{"default_revenue_account_id": "active revenue account not found"}}
		}
		if err != nil {
			return nil, fmt.Errorf("validate default revenue account: %w", err)
		}
	}
	var accountID any
	if c.DefaultRevenueAccountID != 0 {
		accountID = c.DefaultRevenueAccountID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO company_profile (id,name,tagline,address,phone,email,npwp,bank_name,bank_account_number,bank_account_holder,invoice_footer,default_revenue_account_id,recurring_description_template,updated_at) VALUES (1,?,?,?,?,?,?,?,?,?,?,?,?,datetime('now')) ON CONFLICT(id) DO UPDATE SET name=excluded.name,tagline=excluded.tagline,address=excluded.address,phone=excluded.phone,email=excluded.email,npwp=excluded.npwp,bank_name=excluded.bank_name,bank_account_number=excluded.bank_account_number,bank_account_holder=excluded.bank_account_holder,invoice_footer=excluded.invoice_footer,default_revenue_account_id=excluded.default_revenue_account_id,recurring_description_template=excluded.recurring_description_template,updated_at=datetime('now')`, c.Name, c.Tagline, c.Address, c.Phone, c.Email, c.NPWP, c.BankName, c.BankAccountNumber, c.BankAccountHolder, c.InvoiceFooter, accountID, c.RecurringDescriptionTemplate)
	if err != nil {
		return nil, fmt.Errorf("update company profile: %w", err)
	}
	updated, err := getWith(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit company profile update: %w", err)
	}
	metadata := map[string]any{"after": snapshot(updated)}
	if old != nil {
		metadata = audit.Diff(snapshot(old), snapshot(updated), []string{"name", "tagline", "address", "phone", "email", "npwp", "bank_name", "bank_account_holder", "invoice_footer", "default_revenue_account_id", "recurring_description_template"})
	}
	if metadata != nil {
		audit.Log(ctx, m.db, audit.Event{Action: "company_profile.update", TargetType: "company_profile", TargetID: 1, TargetLabel: updated.Name, Metadata: metadata, ActorID: int64(actor.UserID)})
	}
	return updated, nil
}

func snapshot(c *model.CompanyProfile) map[string]any {
	return map[string]any{"name": c.Name, "tagline": c.Tagline, "address": c.Address, "phone": c.Phone, "email": c.Email, "npwp": c.NPWP, "bank_name": c.BankName, "bank_account_holder": c.BankAccountHolder, "invoice_footer": c.InvoiceFooter, "default_revenue_account_id": c.DefaultRevenueAccountID, "recurring_description_template": c.RecurringDescriptionTemplate}
}
