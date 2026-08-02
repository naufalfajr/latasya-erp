package account

import (
	"context"
	"fmt"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

var validTypes = map[string]bool{
	model.AccountTypeAsset: true, model.AccountTypeLiability: true,
	model.AccountTypeEquity: true, model.AccountTypeRevenue: true,
	model.AccountTypeExpense: true,
}

func validate(d Draft) error {
	fields := map[string]string{}
	if strings.TrimSpace(d.Code) == "" {
		fields["code"] = "required"
	}
	if strings.TrimSpace(d.Name) == "" {
		fields["name"] = "required"
	}
	if d.AccountType == "" {
		fields["account_type"] = "required"
	} else if !validTypes[d.AccountType] {
		fields["account_type"] = "must be one of: asset, liability, equity, revenue, expense"
	}
	if d.NormalBalance == "" {
		fields["normal_balance"] = "required"
	} else if d.NormalBalance != "debit" && d.NormalBalance != "credit" {
		fields["normal_balance"] = "must be one of: debit, credit"
	}
	if d.IsCash && (d.AccountType != model.AccountTypeAsset || d.NormalBalance != "debit") {
		fields["is_cash"] = "cash accounts must be debit-normal assets"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func (m *Module) Create(ctx context.Context, actor Actor, d Draft) (*model.Account, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validate(d); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin account create: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO accounts (code,name,account_type,normal_balance,is_active,is_cash,description) VALUES (?,?,?,?,?,?,?)`,
		d.Code, d.Name, d.AccountType, d.NormalBalance, d.IsActive, d.IsCash, d.Description)
	if err != nil {
		if isUnique(err) {
			return nil, &ConflictError{Message: "account code already exists"}
		}
		return nil, fmt.Errorf("create account: %w", err)
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("account id: %w", err)
	}
	created, err := getWith(ctx, tx, int(id64))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account create: %w", err)
	}
	m.audit(ctx, actor, "account.create", created, map[string]any{"after": snapshot(created)})
	return created, nil
}

func (m *Module) Update(ctx context.Context, actor Actor, id int, d Draft) (*model.Account, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	if err := validate(d); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin account update: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE accounts SET id=id WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("lock account: %w", err)
	}
	old, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if old.IsSystem {
		fields := map[string]string{}
		if d.Code != old.Code {
			fields["code"] = "system account code cannot be changed"
		}
		if d.AccountType != old.AccountType {
			fields["account_type"] = "system account type cannot be changed"
		}
		if d.NormalBalance != old.NormalBalance {
			fields["normal_balance"] = "system account normal balance cannot be changed"
		}
		if d.IsActive != old.IsActive {
			fields["is_active"] = "system account active status cannot be changed"
		}
		if d.IsCash != old.IsCash {
			fields["is_cash"] = "system account cash classification cannot be changed"
		}
		if len(fields) > 0 {
			return nil, &ValidationError{Fields: fields}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET code=?,name=?,account_type=?,normal_balance=?,is_active=?,is_cash=?,description=?,updated_at=datetime('now') WHERE id=?`,
		d.Code, d.Name, d.AccountType, d.NormalBalance, d.IsActive, d.IsCash, d.Description, id)
	if err != nil {
		if isUnique(err) {
			return nil, &ConflictError{Message: "account code already exists"}
		}
		return nil, fmt.Errorf("update account: %w", err)
	}
	updated, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account update: %w", err)
	}
	if diff := audit.Diff(snapshot(old), snapshot(updated), []string{"code", "name", "account_type", "normal_balance", "description", "is_active", "is_cash"}); diff != nil {
		audit.Log(ctx, m.db, audit.Event{Action: "account.update", TargetType: "account", TargetID: int64(updated.ID), TargetLabel: old.Code, Metadata: diff, ActorID: int64(actor.UserID)})
	}
	return updated, nil
}

func (m *Module) Delete(ctx context.Context, actor Actor, id int) (*model.Account, error) {
	if err := requireManager(actor); err != nil {
		return nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin account delete: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE accounts SET id=id WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("lock account: %w", err)
	}
	old, err := getWith(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if old.IsSystem {
		return nil, &ConflictError{Message: "cannot delete system account"}
	}
	var linked int
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM accounts WHERE parent_id=?) +
		(SELECT COUNT(*) FROM journal_lines WHERE account_id=?) +
		(SELECT COUNT(*) FROM invoice_lines WHERE account_id=?) +
		(SELECT COUNT(*) FROM bill_lines WHERE account_id=?) +
		(SELECT COUNT(*) FROM payments WHERE account_id=?) +
		(SELECT COUNT(*) FROM credit_note_lines WHERE account_id=?) +
		(SELECT COUNT(*) FROM company_profile WHERE default_revenue_account_id=?)`, id, id, id, id, id, id, id).Scan(&linked); err != nil {
		return nil, fmt.Errorf("count account references: %w", err)
	}
	if linked > 0 {
		return nil, &ConflictError{Message: "account is in use and cannot be deleted"}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM accounts WHERE id=?", id); err != nil {
		return nil, fmt.Errorf("delete account: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit account delete: %w", err)
	}
	m.audit(ctx, actor, "account.delete", old, map[string]any{"before": snapshot(old)})
	return old, nil
}

func snapshot(a *model.Account) map[string]any {
	return map[string]any{"code": a.Code, "name": a.Name, "account_type": a.AccountType, "normal_balance": a.NormalBalance, "description": a.Description, "is_active": a.IsActive, "is_cash": a.IsCash}
}

func (m *Module) audit(ctx context.Context, actor Actor, action string, a *model.Account, metadata map[string]any) {
	audit.Log(ctx, m.db, audit.Event{Action: action, TargetType: "account", TargetID: int64(a.ID), TargetLabel: a.Code, Metadata: metadata, ActorID: int64(actor.UserID)})
}

func isUnique(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique")
}
