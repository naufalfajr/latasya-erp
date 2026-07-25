package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/model"
)

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	granularity, err := model.ParseDashboardGranularity(query.Get("granularity"), query.Has("granularity"))
	if err != nil {
		http.Error(w, "Invalid granularity parameter: use monthly or quarterly", http.StatusBadRequest)
		return
	}
	data, err := model.GetDashboardDataAt(h.DB, granularity, model.BusinessNow())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/dashboard/index.html", "Dashboard", data)
}
