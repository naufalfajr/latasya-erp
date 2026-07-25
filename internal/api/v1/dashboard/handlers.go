package dashboard

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct {
	DB *sql.DB
}

func idr(n int) string {
	return fmt.Sprintf("%d", n)
}

type recentTransactionResp struct {
	ID          int    `json:"id"`
	EntryDate   string `json:"entry_date"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
	Amount      string `json:"amount"`
	SourceType  string `json:"source_type"`
}

type dashboardResp struct {
	CashBalance         *string                 `json:"cash_balance"`
	CashConfigured      bool                    `json:"cash_configured"`
	MonthlyRevenue      string                  `json:"monthly_revenue"`
	MonthlyExpenses     string                  `json:"monthly_expenses"`
	OutstandingInvoices string                  `json:"outstanding_invoices"`
	OutstandingBills    string                  `json:"outstanding_bills"`
	RecentTransactions  []recentTransactionResp `json:"recent_transactions"`
	Months              int                     `json:"months"`
	AsOf                string                  `json:"as_of"`
	MonthlyTrends       []monthlyTrendResp      `json:"monthly_trends"`
}

type monthlyTrendResp struct {
	Month           string  `json:"month"`
	StartDate       string  `json:"start_date"`
	EndDate         string  `json:"end_date"`
	IsPartial       bool    `json:"is_partial"`
	Revenue         string  `json:"revenue"`
	Expenses        string  `json:"expenses"`
	NetIncome       string  `json:"net_income"`
	NetCashMovement *string `json:"net_cash_movement"`
	ClosingCash     *string `json:"closing_cash"`
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	months, err := dashboardMonths(r)
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, err.Error(), map[string]string{
			"months": "must be one of: 6, 12, 24",
		})
		return
	}
	data, err := model.GetDashboardDataAt(h.DB, months, model.BusinessNow())
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to get dashboard data", nil)
		return
	}

	resp := dashboardResp{
		CashBalance:         idrPtr(data.CashBalance),
		CashConfigured:      data.CashConfigured,
		MonthlyRevenue:      idr(data.MonthlyRevenue),
		MonthlyExpenses:     idr(data.MonthlyExpenses),
		OutstandingInvoices: idr(data.OutstandingInvoices),
		OutstandingBills:    idr(data.OutstandingBills),
		RecentTransactions:  make([]recentTransactionResp, 0, len(data.RecentTransactions)),
		Months:              data.Months,
		AsOf:                data.AsOf,
		MonthlyTrends:       make([]monthlyTrendResp, 0, len(data.MonthlyTrends)),
	}
	for _, t := range data.RecentTransactions {
		resp.RecentTransactions = append(resp.RecentTransactions, recentTransactionResp{
			ID:          t.ID,
			EntryDate:   t.EntryDate,
			Reference:   t.Reference,
			Description: t.Description,
			Amount:      idr(t.Amount),
			SourceType:  t.SourceType,
		})
	}
	for _, trend := range data.MonthlyTrends {
		resp.MonthlyTrends = append(resp.MonthlyTrends, monthlyTrendResp{
			Month: trend.Month, StartDate: trend.StartDate, EndDate: trend.EndDate,
			IsPartial: trend.IsPartial, Revenue: idr(trend.Revenue),
			Expenses: idr(trend.Expenses), NetIncome: idr(trend.NetIncome),
			NetCashMovement: idrPtr(trend.NetCashMovement), ClosingCash: idrPtr(trend.ClosingCash),
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func dashboardMonths(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("months")
	if raw == "" {
		return 12, nil
	}
	months, err := strconv.Atoi(raw)
	if err != nil || (months != 6 && months != 12 && months != 24) {
		return 0, fmt.Errorf("unsupported dashboard range")
	}
	return months, nil
}

func idrPtr(n *int) *string {
	if n == nil {
		return nil
	}
	s := idr(*n)
	return &s
}
