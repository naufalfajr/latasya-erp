package model

import (
	"database/sql"
	"fmt"
	"time"
)

type DashboardData struct {
	CashBalance         int
	MonthlyRevenue      int
	MonthlyExpenses     int
	OutstandingInvoices int
	OutstandingBills    int
	RecentTransactions  []RecentTransaction
}

type RecentTransaction struct {
	ID          int
	EntryDate   string
	Reference   string
	Description string
	Amount      int
	SourceType  string
}

func GetDashboardData(db *sql.DB) (*DashboardData, error) {
	d := &DashboardData{}

	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
	today := now.Format("2006-01-02")

	// Cash balance (sum of all cash/bank accounts: code starting with 1-1)
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(jl.debit) - SUM(jl.credit), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id
		WHERE a.code LIKE '1-1%'
	`).Scan(&d.CashBalance); err != nil {
		return nil, fmt.Errorf("cash balance: %w", err)
	}

	// Monthly revenue
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(jl.credit) - SUM(jl.debit), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id
		WHERE a.account_type = 'revenue' AND je.entry_date >= ? AND je.entry_date <= ?
	`, monthStart, today).Scan(&d.MonthlyRevenue); err != nil {
		return nil, fmt.Errorf("monthly revenue: %w", err)
	}

	// Monthly expenses
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(jl.debit) - SUM(jl.credit), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id
		WHERE a.account_type = 'expense' AND je.entry_date >= ? AND je.entry_date <= ?
	`, monthStart, today).Scan(&d.MonthlyExpenses); err != nil {
		return nil, fmt.Errorf("monthly expenses: %w", err)
	}

	// Outstanding invoices (amount due)
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(total - amount_paid), 0)
		FROM invoices WHERE status IN ('sent', 'partial', 'overdue')
	`).Scan(&d.OutstandingInvoices); err != nil {
		return nil, fmt.Errorf("outstanding invoices: %w", err)
	}

	// Outstanding bills (amount due)
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(total - amount_paid), 0)
		FROM bills WHERE status IN ('received', 'partial', 'overdue')
	`).Scan(&d.OutstandingBills); err != nil {
		return nil, fmt.Errorf("outstanding bills: %w", err)
	}

	// Recent transactions (last 10)
	rows, err := db.Query(`
		SELECT je.id, je.entry_date, COALESCE(je.reference,''), je.description,
			COALESCE(SUM(jl.debit), 0),
			COALESCE(je.source_type, 'manual')
		FROM journal_entries je
		LEFT JOIN journal_lines jl ON jl.entry_id = je.id
		WHERE je.is_posted = 1
		GROUP BY je.id, je.entry_date, je.reference, je.description, je.source_type
		ORDER BY je.entry_date DESC, je.id DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, fmt.Errorf("recent transactions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var t RecentTransaction
		if err := rows.Scan(&t.ID, &t.EntryDate, &t.Reference, &t.Description, &t.Amount, &t.SourceType); err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		d.RecentTransactions = append(d.RecentTransactions, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transactions: %w", err)
	}

	return d, nil
}
