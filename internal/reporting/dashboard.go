package reporting

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

var jakartaLocation = time.FixedZone("Asia/Jakarta", 7*60*60)

const dashboardPeriods = 6

type DashboardData struct {
	CashBalance         *int                `json:"cash_balance"`
	CashConfigured      bool                `json:"cash_configured"`
	MonthlyRevenue      int                 `json:"monthly_revenue"`
	MonthlyExpenses     int                 `json:"monthly_expenses"`
	OutstandingInvoices int                 `json:"outstanding_invoices"`
	OutstandingBills    int                 `json:"outstanding_bills"`
	RecentTransactions  []RecentTransaction `json:"recent_transactions"`
	Granularity         string              `json:"granularity"`
	AsOf                string              `json:"as_of"`
	Trends              []PeriodTrend       `json:"trends"`
}

// PeriodTrend summarizes one monthly or quarterly period. The final entry can
// be an in-progress (partial) period.
type PeriodTrend struct {
	Label           string `json:"label"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	IsPartial       bool   `json:"is_partial"`
	Revenue         int    `json:"revenue"`
	Expenses        int    `json:"expenses"`
	NetIncome       int    `json:"net_income"`
	NetCashMovement *int   `json:"net_cash_movement"`
	ClosingCash     *int   `json:"closing_cash"`
}

// monthTrend is the raw calendar-month building block that PeriodTrend rows
// are aggregated from (one-to-one for monthly granularity, three-to-one for
// quarterly).
type monthTrend struct {
	Month           string
	StartDate       string
	EndDate         string
	IsPartial       bool
	Revenue         int
	Expenses        int
	NetIncome       int
	NetCashMovement *int
	ClosingCash     *int
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

// ParseDashboardGranularity validates the dashboard's period-size query
// parameter, defaulting to "monthly" when absent.
func ParseDashboardGranularity(raw string, present bool) (string, error) {
	if !present {
		return "monthly", nil
	}
	if raw != "monthly" && raw != "quarterly" {
		return "", fmt.Errorf("unsupported dashboard granularity")
	}
	return raw, nil
}

// DashboardAt returns dashboard values through the supplied instant,
// interpreted in the Asia/Jakarta business timezone. It always returns the
// most recent dashboardPeriods periods (monthly or quarterly), including the
// current in-progress period.
func (m *Module) DashboardAt(ctx context.Context, granularity string, at time.Time) (*DashboardData, error) {
	bucketSize := 1
	if granularity == "quarterly" {
		bucketSize = 3
	}
	asOf := at.In(jakartaLocation)
	asOfDate := asOf.Format("2006-01-02")
	currentMonthStart := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, jakartaLocation)
	// The current (possibly in-progress) bucket may only have its first few
	// calendar months elapsed so far; count those, then add whole buckets for
	// the rest, so rangeStart always lands on a bucket boundary (a calendar
	// quarter start, for quarterly) instead of a trailing N-month window.
	monthsElapsed := (int(asOf.Month())-1)%bucketSize + 1
	monthsNeeded := (dashboardPeriods-1)*bucketSize + monthsElapsed
	rangeStart := currentMonthStart.AddDate(0, -(monthsNeeded - 1), 0)

	d := &DashboardData{
		Granularity: granularity,
		AsOf:        asOfDate,
	}
	months, cashConfigured, err := monthlyFinancialTrends(ctx, m.db, rangeStart, asOf, monthsNeeded)
	if err != nil {
		return nil, err
	}
	d.Trends = groupIntoPeriods(months, bucketSize, cashConfigured)
	d.CashConfigured = cashConfigured
	if cashConfigured {
		d.CashBalance = d.Trends[len(d.Trends)-1].ClosingCash
	}
	current := d.Trends[len(d.Trends)-1]
	d.MonthlyRevenue = current.Revenue
	d.MonthlyExpenses = current.Expenses

	if err := m.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(total - amount_paid), 0)
		FROM invoices WHERE status IN ('sent', 'partial', 'overdue')
	`).Scan(&d.OutstandingInvoices); err != nil {
		return nil, fmt.Errorf("outstanding invoices: %w", err)
	}
	if err := m.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(total - amount_paid), 0)
		FROM bills WHERE status IN ('received', 'partial', 'overdue')
	`).Scan(&d.OutstandingBills); err != nil {
		return nil, fmt.Errorf("outstanding bills: %w", err)
	}

	rows, err := m.db.QueryContext(ctx, `
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

// groupIntoPeriods aggregates consecutive calendar months into bucketSize-wide
// periods (1 = monthly, 3 = quarterly). The final bucket may hold fewer than
// bucketSize months when the current quarter has just started.
func groupIntoPeriods(months []monthTrend, bucketSize int, cashConfigured bool) []PeriodTrend {
	periods := make([]PeriodTrend, 0, dashboardPeriods)
	i := 0
	for len(periods) < dashboardPeriods-1 {
		periods = append(periods, aggregateBucket(months[i:i+bucketSize], bucketSize, cashConfigured))
		i += bucketSize
	}
	periods = append(periods, aggregateBucket(months[i:], bucketSize, cashConfigured))
	return periods
}

func aggregateBucket(bucket []monthTrend, bucketSize int, cashConfigured bool) PeriodTrend {
	last := bucket[len(bucket)-1]
	trend := PeriodTrend{
		Label:     periodLabel(bucket, bucketSize),
		StartDate: bucket[0].StartDate,
		EndDate:   last.EndDate,
		IsPartial: last.IsPartial,
	}
	for _, m := range bucket {
		trend.Revenue += m.Revenue
		trend.Expenses += m.Expenses
		trend.NetIncome += m.NetIncome
	}
	if cashConfigured {
		movement := 0
		for _, m := range bucket {
			movement += *m.NetCashMovement
		}
		trend.NetCashMovement = intPtr(movement)
		trend.ClosingCash = intPtr(*last.ClosingCash)
	}
	return trend
}

func periodLabel(bucket []monthTrend, bucketSize int) string {
	start, err := time.Parse("2006-01-02", bucket[0].StartDate)
	if err != nil {
		return bucket[0].Month
	}
	if bucketSize == 1 {
		return start.Format("Jan 2006")
	}
	quarter := (int(start.Month())-1)/3 + 1
	return fmt.Sprintf("Q%d %d", quarter, start.Year())
}

func monthlyFinancialTrends(ctx context.Context, db *sql.DB, rangeStart, asOf time.Time, months int) ([]monthTrend, bool, error) {
	startDate := rangeStart.Format("2006-01-02")
	asOfDate := asOf.Format("2006-01-02")
	trends := make([]monthTrend, months)
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
		trends[i] = monthTrend{
			Month: key, StartDate: start.Format("2006-01-02"),
			EndDate: end.Format("2006-01-02"), IsPartial: partial,
		}
		monthIndex[key] = i
	}

	rows, err := db.QueryContext(ctx, `
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
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE is_cash = 1`).Scan(&cashCount); err != nil {
		return nil, false, fmt.Errorf("cash configuration: %w", err)
	}
	if cashCount == 0 {
		return trends, false, nil
	}

	var closing int
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(jl.debit - jl.credit), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.entry_id AND je.is_posted = 1
		JOIN accounts a ON a.id = jl.account_id AND a.is_cash = 1
		WHERE je.entry_date < ?
	`, startDate).Scan(&closing); err != nil {
		return nil, false, fmt.Errorf("opening cash: %w", err)
	}

	movements := make(map[string]int, months)
	rows, err = db.QueryContext(ctx, `
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
