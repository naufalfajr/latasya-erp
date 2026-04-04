package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/model"
)

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	data, err := model.GetDashboardData(h.DB)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/dashboard/index.html", "Dashboard", data)
}
