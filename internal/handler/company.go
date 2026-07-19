package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type companyProfileFormData struct {
	Company         *model.CompanyProfile
	RevenueAccounts []model.Account
	Errors          map[string]string
}

func (h *Handler) CompanyProfilePage(w http.ResponseWriter, r *http.Request) {
	company, err := model.GetCompanyProfile(h.DB)
	if err != nil {
		slog.Error("company_profile: get", "error", err)
		h.render(w, r, "templates/settings/company.html", "Company Profile", companyProfileFormData{
			Company: &model.CompanyProfile{},
			Errors:  map[string]string{"general": "Failed to load company profile"},
		})
		return
	}
	active := true
	revenueAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "revenue", IsActive: &active})
	h.render(w, r, "templates/settings/company.html", "Company Profile", companyProfileFormData{
		Company:         company,
		RevenueAccounts: revenueAccounts,
		Errors:          map[string]string{},
	})
}

func (h *Handler) UpdateCompanyProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	defaultRevenueAccountID, _ := strconv.Atoi(r.FormValue("default_revenue_account_id"))
	company := &model.CompanyProfile{
		Name:                         strings.TrimSpace(r.FormValue("name")),
		Tagline:                      strings.TrimSpace(r.FormValue("tagline")),
		Address:                      strings.TrimSpace(r.FormValue("address")),
		Phone:                        strings.TrimSpace(r.FormValue("phone")),
		Email:                        strings.TrimSpace(r.FormValue("email")),
		NPWP:                         strings.TrimSpace(r.FormValue("npwp")),
		BankName:                     strings.TrimSpace(r.FormValue("bank_name")),
		BankAccountNumber:            strings.TrimSpace(r.FormValue("bank_account_number")),
		BankAccountHolder:            strings.TrimSpace(r.FormValue("bank_account_holder")),
		InvoiceFooter:                strings.TrimSpace(r.FormValue("invoice_footer")),
		DefaultRevenueAccountID:      defaultRevenueAccountID,
		RecurringDescriptionTemplate: strings.TrimSpace(r.FormValue("recurring_description_template")),
	}

	reRender := func(errs map[string]string) {
		active := true
		revenueAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "revenue", IsActive: &active})
		h.render(w, r, "templates/settings/company.html", "Company Profile", companyProfileFormData{
			Company:         company,
			RevenueAccounts: revenueAccounts,
			Errors:          errs,
		})
	}

	if company.Name == "" {
		reRender(map[string]string{"name": "Company name is required"})
		return
	}

	if err := model.UpdateCompanyProfile(h.DB, company); err != nil {
		slog.Error("company_profile: update", "error", err)
		reRender(map[string]string{"general": "Failed to save company profile"})
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "company_profile.update",
		TargetType:  "company_profile",
		TargetID:    1,
		TargetLabel: company.Name,
		Metadata: map[string]any{
			"after": map[string]any{
				"name":      company.Name,
				"npwp":      company.NPWP,
				"bank_name": company.BankName,
			},
		},
	})

	h.setFlash(w, "Company profile saved")
	http.Redirect(w, r, h.BasePath+"/settings/company", http.StatusSeeOther)
}
