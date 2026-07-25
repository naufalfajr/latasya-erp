package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/model"
)

type directionalSignal struct {
	Available  bool
	Current    int
	Difference int
	Negative   bool
}

type dashboardPageData struct {
	*model.DashboardData
	NetIncomeSignal directionalSignal
	CashSignal      directionalSignal
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	months, err := model.ParseDashboardMonths(r.URL.Query().Get("months"))
	if err != nil {
		http.Error(w, "Invalid months parameter: use 6, 12, or 24", http.StatusBadRequest)
		return
	}
	data, err := model.GetDashboardDataAt(h.DB, months, model.BusinessNow())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	page := dashboardPageData{DashboardData: data}
	page.NetIncomeSignal = completedSignal(data.MonthlyTrends, data.ProfitHistoryStart, func(t model.MonthlyTrend) *int {
		return &t.NetIncome
	})
	page.CashSignal = completedSignal(data.MonthlyTrends, data.CashHistoryStart, func(t model.MonthlyTrend) *int {
		return t.ClosingCash
	})
	h.render(w, r, "templates/dashboard/index.html", "Dashboard", page)
}

func completedSignal(trends []model.MonthlyTrend, historyStart string, value func(model.MonthlyTrend) *int) directionalSignal {
	if historyStart == "" {
		return directionalSignal{}
	}
	values := make([]int, 0, 2)
	for i := len(trends) - 1; i >= 0 && len(values) < 2; i-- {
		if trends[i].IsPartial || trends[i].EndDate < historyStart {
			continue
		}
		if v := value(trends[i]); v != nil {
			values = append(values, *v)
		}
	}
	if len(values) < 2 {
		return directionalSignal{}
	}
	return directionalSignal{
		Available: true, Current: values[0],
		Difference: values[0] - values[1], Negative: values[0] < 0,
	}
}
