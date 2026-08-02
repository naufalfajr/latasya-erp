package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/naufal/latasya-erp/internal/model"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func where(f Filter) (string, []any) {
	clause := ""
	args := []any{}
	if f.Type != "" {
		clause += " AND account_type=?"
		args = append(args, f.Type)
	}
	if f.IsActive != nil {
		clause += " AND is_active=?"
		args = append(args, *f.IsActive)
	}
	if f.Search != "" {
		clause += " AND (code LIKE ? OR name LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s)
	}
	return clause, args
}

func (m *Module) List(ctx context.Context, f Filter) (*ListResult, error) {
	clause, args := where(f)
	var total int
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts WHERE 1=1"+clause, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count accounts: %w", err)
	}
	query := `SELECT id,code,name,account_type,normal_balance,parent_id,is_system,is_active,is_cash,COALESCE(description,''),created_at,updated_at
		FROM accounts WHERE 1=1` + clause + ` ORDER BY code`
	listArgs := append([]any(nil), args...)
	if f.Limit > 0 {
		query += " LIMIT ? OFFSET ?"
		listArgs = append(listArgs, f.Limit, f.Offset)
	}
	rows, err := m.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()
	accounts := []model.Account{}
	for rows.Next() {
		var a model.Account
		if err := scanAccount(rows, &a); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		accounts = append(accounts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate accounts: %w", err)
	}
	return &ListResult{Accounts: accounts, Total: total}, nil
}

func (m *Module) TypeCounts(ctx context.Context, activeOnly bool) (map[string]int, error) {
	query := "SELECT account_type,COUNT(*) FROM accounts"
	if activeOnly {
		query += " WHERE is_active=1"
	}
	query += " GROUP BY account_type"
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("count account types: %w", err)
	}
	defer rows.Close()
	counts := map[string]int{"all": 0}
	for rows.Next() {
		var typ string
		var count int
		if err := rows.Scan(&typ, &count); err != nil {
			return nil, fmt.Errorf("scan account type count: %w", err)
		}
		counts[typ] = count
		counts["all"] += count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account type counts: %w", err)
	}
	return counts, nil
}

type scanner interface{ Scan(...any) error }

func scanAccount(row scanner, a *model.Account) error {
	return row.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.NormalBalance, &a.ParentID,
		&a.IsSystem, &a.IsActive, &a.IsCash, &a.Description, &a.CreatedAt, &a.UpdatedAt)
}

func getWith(ctx context.Context, q queryer, id int) (*model.Account, error) {
	a := &model.Account{}
	err := scanAccount(q.QueryRowContext(ctx, `SELECT id,code,name,account_type,normal_balance,parent_id,is_system,is_active,is_cash,COALESCE(description,''),created_at,updated_at FROM accounts WHERE id=?`, id), a)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	return a, nil
}

func (m *Module) Get(ctx context.Context, id int) (*model.Account, error) {
	return getWith(ctx, m.db, id)
}
