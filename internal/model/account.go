package model

import (
	"database/sql"
	"fmt"
)

type Account struct {
	ID            int
	Code          string
	Name          string
	AccountType   string
	NormalBalance string
	ParentID      *int
	IsSystem      bool
	IsActive      bool
	Description   string
	CreatedAt     string
	UpdatedAt     string
}

type AccountFilter struct {
	Type     string // asset, liability, equity, revenue, expense
	IsActive *bool
	Search   string
}

func ListAccounts(db *sql.DB, f AccountFilter) ([]Account, error) {
	query := "SELECT id, code, name, account_type, normal_balance, parent_id, is_system, is_active, COALESCE(description,''), created_at, updated_at FROM accounts WHERE 1=1"
	var args []any

	if f.Type != "" {
		query += " AND account_type = ?"
		args = append(args, f.Type)
	}
	if f.IsActive != nil {
		query += " AND is_active = ?"
		if *f.IsActive {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if f.Search != "" {
		query += " AND (code LIKE ? OR name LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s)
	}
	query += " ORDER BY code"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.NormalBalance,
			&a.ParentID, &a.IsSystem, &a.IsActive, &a.Description, &a.CreatedAt, &a.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func GetAccount(db *sql.DB, id int) (*Account, error) {
	a := &Account{}
	err := db.QueryRow(
		"SELECT id, code, name, account_type, normal_balance, parent_id, is_system, is_active, COALESCE(description,''), created_at, updated_at FROM accounts WHERE id = ?",
		id,
	).Scan(&a.ID, &a.Code, &a.Name, &a.AccountType, &a.NormalBalance,
		&a.ParentID, &a.IsSystem, &a.IsActive, &a.Description, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	return a, nil
}

func CreateAccount(db *sql.DB, a *Account) error {
	_, err := db.Exec(
		"INSERT INTO accounts (code, name, account_type, normal_balance, parent_id, is_system, is_active, description) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		a.Code, a.Name, a.AccountType, a.NormalBalance, a.ParentID, a.IsSystem, a.IsActive, a.Description,
	)
	if err != nil {
		return fmt.Errorf("create account: %w", err)
	}
	return nil
}

func UpdateAccount(db *sql.DB, a *Account) error {
	_, err := db.Exec(
		"UPDATE accounts SET code=?, name=?, account_type=?, normal_balance=?, parent_id=?, is_active=?, description=?, updated_at=datetime('now') WHERE id=?",
		a.Code, a.Name, a.AccountType, a.NormalBalance, a.ParentID, a.IsActive, a.Description, a.ID,
	)
	if err != nil {
		return fmt.Errorf("update account: %w", err)
	}
	return nil
}

func DeleteAccount(db *sql.DB, id int) error {
	_, err := db.Exec("DELETE FROM accounts WHERE id = ? AND is_system = 0", id)
	if err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return nil
}

// AccountTypeLabel returns a human-readable label for account types
func AccountTypeLabel(t string) string {
	switch t {
	case "asset":
		return "Asset"
	case "liability":
		return "Liability"
	case "equity":
		return "Equity"
	case "revenue":
		return "Revenue"
	case "expense":
		return "Expense"
	default:
		return t
	}
}
