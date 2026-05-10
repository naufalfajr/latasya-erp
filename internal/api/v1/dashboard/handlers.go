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
	CashBalance         string                  `json:"cash_balance"`
	MonthlyRevenue      string                  `json:"monthly_revenue"`
	MonthlyExpenses     string                  `json:"monthly_expenses"`
	OutstandingInvoices string                  `json:"outstanding_invoices"`
	OutstandingBills    string                  `json:"outstanding_bills"`
	RecentTransactions  []recentTransactionResp `json:"recent_transactions"`
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	data, err := model.GetDashboardData(h.DB)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to get dashboard data", nil)
		return
	}

	resp := dashboardResp{
		CashBalance:         idr(data.CashBalance),
		MonthlyRevenue:      idr(data.MonthlyRevenue),
		MonthlyExpenses:     idr(data.MonthlyExpenses),
		OutstandingInvoices: idr(data.OutstandingInvoices),
		OutstandingBills:    idr(data.OutstandingBills),
		RecentTransactions:  make([]recentTransactionResp, 0, len(data.RecentTransactions)),
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

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": resp})
}
