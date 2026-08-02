package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/auth"
	companyModule "github.com/naufal/latasya-erp/internal/company"
	"github.com/naufal/latasya-erp/internal/model"
)

type companyProfileFormData struct {
	Company         *model.CompanyProfile
	RevenueAccounts []model.Account
	Errors          map[string]string
}

func (h *Handler) companyRevenueAccounts(r *http.Request) []model.Account {
	active := true
	result, err := h.Accounts.List(r.Context(), account.Filter{Type: model.AccountTypeRevenue, IsActive: &active})
	if err != nil {
		return nil
	}
	return result.Accounts
}

func (h *Handler) CompanyProfilePage(w http.ResponseWriter, r *http.Request) {
	c, err := h.Company.Get(r.Context())
	if err != nil {
		slog.Error("company_profile: get", "error", err)
		h.render(w, r, "templates/settings/company.html", "Company Profile", companyProfileFormData{Company: &model.CompanyProfile{}, Errors: map[string]string{"general": "Failed to load company profile"}})
		return
	}
	h.render(w, r, "templates/settings/company.html", "Company Profile", companyProfileFormData{Company: c, RevenueAccounts: h.companyRevenueAccounts(r), Errors: map[string]string{}})
}

func companyActor(r *http.Request) companyModule.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return companyModule.Actor{}
	}
	return companyModule.Actor{UserID: u.ID, CanManage: u.Role == model.RoleAdmin}
}

func companyFromForm(r *http.Request) model.CompanyProfile {
	accountID, _ := strconv.Atoi(r.FormValue("default_revenue_account_id"))
	return model.CompanyProfile{Name: strings.TrimSpace(r.FormValue("name")), Tagline: strings.TrimSpace(r.FormValue("tagline")), Address: strings.TrimSpace(r.FormValue("address")), Phone: strings.TrimSpace(r.FormValue("phone")), Email: strings.TrimSpace(r.FormValue("email")), NPWP: strings.TrimSpace(r.FormValue("npwp")), BankName: strings.TrimSpace(r.FormValue("bank_name")), BankAccountNumber: strings.TrimSpace(r.FormValue("bank_account_number")), BankAccountHolder: strings.TrimSpace(r.FormValue("bank_account_holder")), InvoiceFooter: strings.TrimSpace(r.FormValue("invoice_footer")), DefaultRevenueAccountID: accountID, RecurringDescriptionTemplate: strings.TrimSpace(r.FormValue("recurring_description_template"))}
}

func (h *Handler) UpdateCompanyProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	c := companyFromForm(r)
	_, err := h.Company.Update(r.Context(), companyActor(r), c)
	if err != nil {
		var validation *companyModule.ValidationError
		if errors.As(err, &validation) {
			fields := map[string]string{}
			for field, message := range validation.Fields {
				if field == "name" && message == "required" {
					message = "Company name is required"
				}
				fields[field] = message
			}
			h.render(w, r, "templates/settings/company.html", "Company Profile", companyProfileFormData{Company: &c, RevenueAccounts: h.companyRevenueAccounts(r), Errors: fields})
			return
		}
		slog.Error("company_profile: update", "error", err)
		h.render(w, r, "templates/settings/company.html", "Company Profile", companyProfileFormData{Company: &c, RevenueAccounts: h.companyRevenueAccounts(r), Errors: map[string]string{"general": "Failed to save company profile"}})
		return
	}
	h.setFlash(w, "Company profile saved")
	http.Redirect(w, r, h.BasePath+"/settings/company", http.StatusSeeOther)
}

func (h *Handler) RegisterCompanyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/company", auth.AdminOnly(h.CompanyProfilePage))
	mux.HandleFunc("POST /settings/company", auth.AdminOnly(h.UpdateCompanyProfile))
}
