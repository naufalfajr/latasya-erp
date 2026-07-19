package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/model"
)

type homeData struct {
	Company   *model.CompanyProfile
	ContactWA string
}

// PublicHome is the bare-domain landing page: a read-only company profile
// for anyone who lands there without an invoice link, plus a discreet path
// to staff login. Parents reach their bill via their own /i/{token} link,
// never through this page.
func (h *Handler) PublicHome(w http.ResponseWriter, r *http.Request) {
	company, err := model.GetCompanyProfile(h.DB)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	t, err := h.getTemplate("templates/home/index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	t.ExecuteTemplate(w, "index.html", PageData{
		Title:    company.Name,
		BasePath: h.BasePath,
		Data: homeData{
			Company:   company,
			ContactWA: buildWALink(company.Phone, "Halo, saya ingin bertanya soal layanan antar jemput Latasya."),
		},
	})
}
