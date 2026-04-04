package handler

import "net/http"

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/dashboard/index.html", "Dashboard", nil)
}
