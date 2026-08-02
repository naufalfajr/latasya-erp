package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/reporting"
)

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	granularity, err := reporting.ParseDashboardGranularity(query.Get("granularity"), query.Has("granularity"))
	if err != nil {
		http.Error(w, "Invalid granularity parameter: use monthly or quarterly", http.StatusBadRequest)
		return
	}
	data, err := h.Reporting.DashboardAt(r.Context(), granularity, reporting.BusinessNow())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/dashboard/index.html", "Dashboard", data)
}
