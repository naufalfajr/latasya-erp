package dashboard

import (
	"database/sql"
	"fmt"
	"net/http"

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
	Granularity         string                  `json:"granularity"`
	AsOf                string                  `json:"as_of"`
	Trends              []periodTrendResp       `json:"trends"`
}

type periodTrendResp struct {
	Label           string  `json:"label"`
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
	query := r.URL.Query()
	granularity, err := model.ParseDashboardGranularity(query.Get("granularity"), query.Has("granularity"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, err.Error(), map[string]string{
			"granularity": "must be one of: monthly, quarterly",
		})
		return
	}
	data, err := model.GetDashboardDataAt(h.DB, granularity, model.BusinessNow())
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
		Granularity:         data.Granularity,
		AsOf:                data.AsOf,
		Trends:              make([]periodTrendResp, 0, len(data.Trends)),
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
	for _, trend := range data.Trends {
		resp.Trends = append(resp.Trends, periodTrendResp{
			Label: trend.Label, StartDate: trend.StartDate, EndDate: trend.EndDate,
			IsPartial: trend.IsPartial, Revenue: idr(trend.Revenue),
			Expenses: idr(trend.Expenses), NetIncome: idr(trend.NetIncome),
			NetCashMovement: idrPtr(trend.NetCashMovement), ClosingCash: idrPtr(trend.ClosingCash),
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": resp})
}

func idrPtr(n *int) *string {
	if n == nil {
		return nil
	}
	s := idr(*n)
	return &s
}
