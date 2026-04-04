package model

import (
	"database/sql"
	"fmt"
)

// TrialBalanceRow represents one account's totals in a trial balance.
type TrialBalanceRow struct {
	AccountID     int
	AccountCode   string
	AccountName   string
	AccountType   string
	NormalBalance string
	TotalDebit    int
	TotalCredit   int
	Balance       int // Positive = debit balance, Negative = credit balance (for display, use absolute value + side)
}

// TrialBalance returns the trial balance for a date range.
// If dateFrom is empty, includes all entries up to dateTo.
func TrialBalance(db *sql.DB, dateFrom, dateTo string) ([]TrialBalanceRow, error) {
	query := `
		SELECT a.id, a.code, a.name, a.account_type, a.normal_balance,
			COALESCE(SUM(jl.debit), 0) AS total_debit,
			COALESCE(SUM(jl.credit), 0) AS total_credit
		FROM accounts a
		LEFT JOIN journal_lines jl ON jl.account_id = a.id
		LEFT JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1`

	var args []any
	whereClauses := ""
	if dateFrom != "" {
		whereClauses += " AND je.entry_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		whereClauses += " AND je.entry_date <= ?"
		args = append(args, dateTo)
	}
	if whereClauses != "" {
		query += " WHERE 1=1" + whereClauses
	}

	query += `
		GROUP BY a.id, a.code, a.name, a.account_type, a.normal_balance
		HAVING COALESCE(SUM(jl.debit), 0) != 0 OR COALESCE(SUM(jl.credit), 0) != 0
		ORDER BY a.code`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("trial balance query: %w", err)
	}
	defer rows.Close()

	var result []TrialBalanceRow
	for rows.Next() {
		var r TrialBalanceRow
		if err := rows.Scan(&r.AccountID, &r.AccountCode, &r.AccountName, &r.AccountType, &r.NormalBalance,
			&r.TotalDebit, &r.TotalCredit); err != nil {
			return nil, fmt.Errorf("scan trial balance: %w", err)
		}
		r.Balance = r.TotalDebit - r.TotalCredit
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trial balance: %w", err)
	}
	return result, nil
}

// ProfitLossRow represents a revenue or expense account in the P&L.
type ProfitLossRow struct {
	AccountCode string
	AccountName string
	AccountType string
	Amount      int // Positive for both revenue and expense
}

// ProfitLossReport holds the full P&L report.
type ProfitLossReport struct {
	Revenue      []ProfitLossRow
	Expenses     []ProfitLossRow
	TotalRevenue int
	TotalExpense int
	NetIncome    int
}

// ProfitLoss returns the profit & loss report for a date range.
func ProfitLoss(db *sql.DB, dateFrom, dateTo string) (*ProfitLossReport, error) {
	query := `
		SELECT a.code, a.name, a.account_type,
			COALESCE(SUM(jl.credit), 0) - COALESCE(SUM(jl.debit), 0) AS net_amount
		FROM accounts a
		JOIN journal_lines jl ON jl.account_id = a.id
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		WHERE a.account_type IN ('revenue', 'expense')`

	var args []any
	if dateFrom != "" {
		query += " AND je.entry_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		query += " AND je.entry_date <= ?"
		args = append(args, dateTo)
	}

	query += `
		GROUP BY a.id, a.code, a.name, a.account_type
		HAVING net_amount != 0
		ORDER BY a.code`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("profit loss query: %w", err)
	}
	defer rows.Close()

	report := &ProfitLossReport{}
	for rows.Next() {
		var code, name, acctType string
		var netAmount int
		if err := rows.Scan(&code, &name, &acctType, &netAmount); err != nil {
			return nil, fmt.Errorf("scan profit loss: %w", err)
		}

		row := ProfitLossRow{AccountCode: code, AccountName: name, AccountType: acctType}
		if acctType == AccountTypeRevenue {
			row.Amount = netAmount // revenue: credit - debit (positive = revenue)
			report.Revenue = append(report.Revenue, row)
			report.TotalRevenue += netAmount
		} else {
			row.Amount = -netAmount // expense: debit - credit (flip sign so positive = expense)
			report.Expenses = append(report.Expenses, row)
			report.TotalExpense += -netAmount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profit loss: %w", err)
	}
	report.NetIncome = report.TotalRevenue - report.TotalExpense
	return report, nil
}

// BalanceSheetSection holds accounts grouped by type.
type BalanceSheetSection struct {
	Accounts []BalanceSheetRow
	Total    int
}

type BalanceSheetRow struct {
	AccountCode string
	AccountName string
	Balance     int
}

// BalanceSheetReport holds the full balance sheet.
type BalanceSheetReport struct {
	Assets          BalanceSheetSection
	Liabilities     BalanceSheetSection
	Equity          BalanceSheetSection
	RetainedEarnings int
	TotalLiabEquity int
}

// BalanceSheet returns the balance sheet as of a specific date.
func BalanceSheet(db *sql.DB, asOfDate string) (*BalanceSheetReport, error) {
	query := `
		SELECT a.code, a.name, a.account_type, a.normal_balance,
			COALESCE(SUM(jl.debit), 0) AS total_debit,
			COALESCE(SUM(jl.credit), 0) AS total_credit
		FROM accounts a
		JOIN journal_lines jl ON jl.account_id = a.id
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		WHERE a.account_type IN ('asset', 'liability', 'equity')
			AND je.entry_date <= ?
		GROUP BY a.id, a.code, a.name, a.account_type, a.normal_balance
		HAVING total_debit != 0 OR total_credit != 0
		ORDER BY a.code`

	rows, err := db.Query(query, asOfDate)
	if err != nil {
		return nil, fmt.Errorf("balance sheet query: %w", err)
	}
	defer rows.Close()

	report := &BalanceSheetReport{}
	for rows.Next() {
		var code, name, acctType, normalBalance string
		var totalDebit, totalCredit int
		if err := rows.Scan(&code, &name, &acctType, &normalBalance, &totalDebit, &totalCredit); err != nil {
			return nil, fmt.Errorf("scan balance sheet: %w", err)
		}

		// Calculate balance based on normal balance side
		var balance int
		if normalBalance == "debit" {
			balance = totalDebit - totalCredit
		} else {
			balance = totalCredit - totalDebit
		}

		row := BalanceSheetRow{AccountCode: code, AccountName: name, Balance: balance}
		switch acctType {
		case AccountTypeAsset:
			report.Assets.Accounts = append(report.Assets.Accounts, row)
			report.Assets.Total += balance
		case AccountTypeLiability:
			report.Liabilities.Accounts = append(report.Liabilities.Accounts, row)
			report.Liabilities.Total += balance
		case AccountTypeEquity:
			report.Equity.Accounts = append(report.Equity.Accounts, row)
			report.Equity.Total += balance
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate balance sheet: %w", err)
	}

	// Calculate retained earnings (net income from revenue - expense accounts)
	pl, err := ProfitLoss(db, "", asOfDate)
	if err != nil {
		return nil, fmt.Errorf("retained earnings: %w", err)
	}
	report.RetainedEarnings = pl.NetIncome
	report.TotalLiabEquity = report.Liabilities.Total + report.Equity.Total + report.RetainedEarnings

	return report, nil
}

// GeneralLedgerEntry represents a single transaction in the general ledger.
type GeneralLedgerEntry struct {
	EntryDate   string
	Reference   string
	Description string
	Debit       int
	Credit      int
	Balance     int
}

// GeneralLedger returns all transactions for a specific account within a date range.
func GeneralLedger(db *sql.DB, accountID int, dateFrom, dateTo string) ([]GeneralLedgerEntry, error) {
	query := `
		SELECT je.entry_date, COALESCE(je.reference,''), je.description, jl.debit, jl.credit
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		WHERE jl.account_id = ?`

	args := []any{accountID}
	if dateFrom != "" {
		query += " AND je.entry_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		query += " AND je.entry_date <= ?"
		args = append(args, dateTo)
	}
	query += " ORDER BY je.entry_date, je.id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("general ledger query: %w", err)
	}
	defer rows.Close()

	var entries []GeneralLedgerEntry
	var runningBalance int
	for rows.Next() {
		var e GeneralLedgerEntry
		if err := rows.Scan(&e.EntryDate, &e.Reference, &e.Description, &e.Debit, &e.Credit); err != nil {
			return nil, fmt.Errorf("scan general ledger: %w", err)
		}
		runningBalance += e.Debit - e.Credit
		e.Balance = runningBalance
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate general ledger: %w", err)
	}
	return entries, nil
}

// CashFlowRow represents one line in the cash flow report.
type CashFlowRow struct {
	AccountCode string
	AccountName string
	Amount      int
}

// CashFlowReport holds the cash flow statement.
type CashFlowReport struct {
	Operating      []CashFlowRow
	TotalOperating int
	NetCashChange  int
	OpeningCash    int
	ClosingCash    int
}

// CashFlow returns a simplified cash flow report for a date range.
// It shows changes in cash/bank accounts categorized by the counterpart account type.
func CashFlow(db *sql.DB, dateFrom, dateTo string) (*CashFlowReport, error) {
	// Get all cash/bank account transactions
	query := `
		SELECT a2.code, a2.name,
			SUM(CASE WHEN jl2.account_id = jl.account_id THEN 0
				WHEN jl2.debit > 0 THEN -jl2.debit
				ELSE jl2.credit END) AS amount
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id
		JOIN journal_lines jl2 ON jl2.entry_id = je.id AND jl2.account_id != jl.account_id
		JOIN accounts a2 ON a2.id = jl2.account_id
		WHERE a.code LIKE '1-1%'`

	var args []any
	if dateFrom != "" {
		query += " AND je.entry_date >= ?"
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		query += " AND je.entry_date <= ?"
		args = append(args, dateTo)
	}

	query += `
		GROUP BY a2.id, a2.code, a2.name
		HAVING amount != 0
		ORDER BY a2.code`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("cash flow query: %w", err)
	}
	defer rows.Close()

	report := &CashFlowReport{}
	for rows.Next() {
		var r CashFlowRow
		if err := rows.Scan(&r.AccountCode, &r.AccountName, &r.Amount); err != nil {
			return nil, fmt.Errorf("scan cash flow: %w", err)
		}
		report.Operating = append(report.Operating, r)
		report.TotalOperating += r.Amount
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cash flow: %w", err)
	}

	// Calculate opening/closing cash balances
	if dateFrom != "" {
		if err := db.QueryRow(`
			SELECT COALESCE(SUM(jl.debit) - SUM(jl.credit), 0)
			FROM journal_lines jl
			JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
			JOIN accounts a ON a.id = jl.account_id
			WHERE a.code LIKE '1-1%' AND je.entry_date < ?`, dateFrom).Scan(&report.OpeningCash); err != nil {
			return nil, fmt.Errorf("opening cash: %w", err)
		}
	}

	closingQuery := `
		SELECT COALESCE(SUM(jl.debit) - SUM(jl.credit), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id
		WHERE a.code LIKE '1-1%'`
	closingArgs := []any{}
	if dateTo != "" {
		closingQuery += " AND je.entry_date <= ?"
		closingArgs = append(closingArgs, dateTo)
	}
	if err := db.QueryRow(closingQuery, closingArgs...).Scan(&report.ClosingCash); err != nil {
		return nil, fmt.Errorf("closing cash: %w", err)
	}

	report.NetCashChange = report.ClosingCash - report.OpeningCash
	return report, nil
}
