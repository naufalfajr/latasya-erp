package model

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

var jakartaLocation = time.FixedZone("Asia/Jakarta", 7*60*60)

type DashboardData struct {
	CashBalance         *int                `json:"cash_balance"`
	CashConfigured      bool                `json:"cash_configured"`
	MonthlyRevenue      int                 `json:"monthly_revenue"`
	MonthlyExpenses     int                 `json:"monthly_expenses"`
	OutstandingInvoices int                 `json:"outstanding_invoices"`
	OutstandingBills    int                 `json:"outstanding_bills"`
	RecentTransactions  []RecentTransaction `json:"recent_transactions"`
	Months              int                 `json:"months"`
	AsOf                string              `json:"as_of"`
	MonthlyTrends       []MonthlyTrend      `json:"monthly_trends"`
	ProfitHistoryStart  string              `json:"-"`
	CashHistoryStart    string              `json:"-"`
}

type MonthlyTrend struct {
	Month           string `json:"month"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	IsPartial       bool   `json:"is_partial"`
	Revenue         int    `json:"revenue"`
	Expenses        int    `json:"expenses"`
	NetIncome       int    `json:"net_income"`
	NetCashMovement *int   `json:"net_cash_movement"`
	ClosingCash     *int   `json:"closing_cash"`
}

type RecentTransaction struct {
	ID          int    `json:"id"`
	EntryDate   string `json:"entry_date"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
	Amount      int    `json:"amount"`
	SourceType  string `json:"source_type"`
}

func BusinessNow() time.Time {
	return time.Now().In(jakartaLocation)
}

func ParseDashboardMonths(raw string, present bool) (int, error) {
	if !present {
		return 12, nil
	}
	months, err := strconv.Atoi(raw)
	if err != nil || (months != 6 && months != 12 && months != 24) {
		return 0, fmt.Errorf("unsupported dashboard range")
	}
	return months, nil
}

func GetDashboardData(db *sql.DB) (*DashboardData, error) {
	return GetDashboardDataAt(db, 12, BusinessNow())
}

// GetDashboardDataAt returns dashboard values through the supplied instant,
// interpreted in the Asia/Jakarta business timezone.
func GetDashboardDataAt(db *sql.DB, months int, at time.Time) (*DashboardData, error) {
	if months < 1 {
		return nil, fmt.Errorf("months must be positive")
	}
	asOf := at.In(jakartaLocation)
	asOfDate := asOf.Format("2006-01-02")
	currentStart := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, jakartaLocation)
	rangeStart := currentStart.AddDate(0, -(months - 1), 0)

	d := &DashboardData{
		Months: months,
		AsOf:   asOfDate,
	}
	trends, cashConfigured, err := monthlyFinancialTrends(db, rangeStart, asOf, months)
	if err != nil {
		return nil, err
	}
	d.MonthlyTrends = trends
	d.CashConfigured = cashConfigured
	if cashConfigured {
		d.CashBalance = trends[len(trends)-1].ClosingCash
	}
	current := trends[len(trends)-1]
	d.MonthlyRevenue = current.Revenue
	d.MonthlyExpenses = current.Expenses
	if err := db.QueryRow(`
		SELECT COALESCE(MIN(je.entry_date), '')
		FROM journal_entries je
		JOIN journal_lines jl ON jl.entry_id = je.id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.is_posted = 1 AND je.entry_date <= ?
			AND a.account_type IN ('revenue', 'expense')
	`, asOfDate).Scan(&d.ProfitHistoryStart); err != nil {
		return nil, fmt.Errorf("profitability history: %w", err)
	}
	if cashConfigured {
		if err := db.QueryRow(`
			SELECT COALESCE(MIN(je.entry_date), '')
			FROM journal_entries je
			JOIN journal_lines jl ON jl.entry_id = je.id
			JOIN accounts a ON a.id = jl.account_id AND a.is_cash = 1
			WHERE je.is_posted = 1 AND je.entry_date <= ?
		`, asOfDate).Scan(&d.CashHistoryStart); err != nil {
			return nil, fmt.Errorf("cash history: %w", err)
		}
	}

	if err := db.QueryRow(`
		SELECT COALESCE(SUM(total - amount_paid), 0)
		FROM invoices WHERE status IN ('sent', 'partial', 'overdue')
	`).Scan(&d.OutstandingInvoices); err != nil {
		return nil, fmt.Errorf("outstanding invoices: %w", err)
	}
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(total - amount_paid), 0)
		FROM bills WHERE status IN ('received', 'partial', 'overdue')
	`).Scan(&d.OutstandingBills); err != nil {
		return nil, fmt.Errorf("outstanding bills: %w", err)
	}

	rows, err := db.Query(`
		SELECT je.id, je.entry_date, COALESCE(je.reference,''), je.description,
			COALESCE(SUM(jl.debit), 0), COALESCE(je.source_type, 'manual')
		FROM journal_entries je
		LEFT JOIN journal_lines jl ON jl.entry_id = je.id
		WHERE je.is_posted = 1 AND je.entry_date <= ?
		GROUP BY je.id, je.entry_date, je.reference, je.description, je.source_type
		ORDER BY je.entry_date DESC, je.id DESC
		LIMIT 10
	`, asOfDate)
	if err != nil {
		return nil, fmt.Errorf("recent transactions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tx RecentTransaction
		if err := rows.Scan(&tx.ID, &tx.EntryDate, &tx.Reference, &tx.Description, &tx.Amount, &tx.SourceType); err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		d.RecentTransactions = append(d.RecentTransactions, tx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transactions: %w", err)
	}
	return d, nil
}

func monthlyFinancialTrends(db *sql.DB, rangeStart, asOf time.Time, months int) ([]MonthlyTrend, bool, error) {
	startDate := rangeStart.Format("2006-01-02")
	asOfDate := asOf.Format("2006-01-02")
	trends := make([]MonthlyTrend, months)
	monthIndex := make(map[string]int, months)
	for i := range months {
		start := rangeStart.AddDate(0, i, 0)
		next := start.AddDate(0, 1, 0)
		end := next.AddDate(0, 0, -1)
		partial := start.Year() == asOf.Year() && start.Month() == asOf.Month()
		if partial {
			end = asOf
		}
		key := start.Format("2006-01")
		trends[i] = MonthlyTrend{
			Month: key, StartDate: start.Format("2006-01-02"),
			EndDate: end.Format("2006-01-02"), IsPartial: partial,
		}
		monthIndex[key] = i
	}

	rows, err := db.Query(`
		SELECT substr(je.entry_date, 1, 7),
			COALESCE(SUM(CASE WHEN a.account_type = 'revenue' THEN jl.credit - jl.debit ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN a.account_type = 'expense' THEN jl.debit - jl.credit ELSE 0 END), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.entry_date >= ? AND je.entry_date <= ?
			AND a.account_type IN ('revenue', 'expense')
		GROUP BY substr(je.entry_date, 1, 7)
	`, startDate, asOfDate)
	if err != nil {
		return nil, false, fmt.Errorf("monthly profitability: %w", err)
	}
	for rows.Next() {
		var month string
		var revenue, expenses int
		if err := rows.Scan(&month, &revenue, &expenses); err != nil {
			rows.Close()
			return nil, false, fmt.Errorf("scan monthly profitability: %w", err)
		}
		if i, ok := monthIndex[month]; ok {
			trends[i].Revenue = revenue
			trends[i].Expenses = expenses
			trends[i].NetIncome = revenue - expenses
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, fmt.Errorf("iterate monthly profitability: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}

	var cashCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM accounts WHERE is_cash = 1`).Scan(&cashCount); err != nil {
		return nil, false, fmt.Errorf("cash configuration: %w", err)
	}
	if cashCount == 0 {
		return trends, false, nil
	}

	var closing int
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(jl.debit - jl.credit), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id AND a.is_cash = 1
		WHERE je.entry_date < ?
	`, startDate).Scan(&closing); err != nil {
		return nil, false, fmt.Errorf("opening cash: %w", err)
	}

	movements := make(map[string]int, months)
	rows, err = db.Query(`
		SELECT substr(je.entry_date, 1, 7), COALESCE(SUM(jl.debit - jl.credit), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id AND a.is_cash = 1
		WHERE je.entry_date >= ? AND je.entry_date <= ?
		GROUP BY substr(je.entry_date, 1, 7)
	`, startDate, asOfDate)
	if err != nil {
		return nil, false, fmt.Errorf("monthly cash movement: %w", err)
	}
	for rows.Next() {
		var month string
		var movement int
		if err := rows.Scan(&month, &movement); err != nil {
			rows.Close()
			return nil, false, fmt.Errorf("scan monthly cash movement: %w", err)
		}
		movements[month] = movement
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, fmt.Errorf("iterate monthly cash movement: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}
	for i := range trends {
		movement := movements[trends[i].Month]
		closing += movement
		trends[i].NetCashMovement = intPtr(movement)
		trends[i].ClosingCash = intPtr(closing)
	}
	return trends, true, nil
}

func intPtr(n int) *int {
	return &n
}
