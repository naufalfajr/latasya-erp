package handler

import (
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/model"
)

type accountPageData struct {
	Accounts    []model.Account
	Filter      string
	Search      string
	TypeCounts  map[string]int
}

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	filterType := r.URL.Query().Get("type")
	search := r.URL.Query().Get("search")

	active := true
	accounts, err := model.ListAccounts(h.DB, model.AccountFilter{
		Type:     filterType,
		IsActive: &active,
		Search:   search,
	})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Count by type for filter tabs
	allAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})
	typeCounts := map[string]int{"all": len(allAccounts)}
	for _, a := range allAccounts {
		typeCounts[a.AccountType]++
	}

	data := accountPageData{
		Accounts:   accounts,
		Filter:     filterType,
		Search:     search,
		TypeCounts: typeCounts,
	}

	h.render(w, r, "templates/accounts/index.html", "Chart of Accounts", data)
}

type accountFormData struct {
	Account *model.Account
	Errors  map[string]string
	IsEdit  bool
}

func (h *Handler) NewAccount(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/accounts/form.html", "New Account", accountFormData{
		Account: &model.Account{IsActive: true},
	})
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	a := &model.Account{
		Code:          r.FormValue("code"),
		Name:          r.FormValue("name"),
		AccountType:   r.FormValue("account_type"),
		NormalBalance: r.FormValue("normal_balance"),
		Description:   r.FormValue("description"),
		IsActive:      r.FormValue("is_active") == "on",
	}

	errors := validateAccount(a)
	if len(errors) > 0 {
		h.render(w, r, "templates/accounts/form.html", "New Account", accountFormData{
			Account: a,
			Errors:  errors,
		})
		return
	}

	if err := model.CreateAccount(h.DB, a); err != nil {
		errors["code"] = "Account code already exists"
		h.render(w, r, "templates/accounts/form.html", "New Account", accountFormData{
			Account: a,
			Errors:  errors,
		})
		return
	}

	h.setFlash(w, "Account created successfully")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (h *Handler) EditAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	account, err := model.GetAccount(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	h.render(w, r, "templates/accounts/form.html", "Edit Account", accountFormData{
		Account: account,
		IsEdit:  true,
	})
}

func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	existing, err := model.GetAccount(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	a := &model.Account{
		ID:            id,
		Code:          r.FormValue("code"),
		Name:          r.FormValue("name"),
		AccountType:   r.FormValue("account_type"),
		NormalBalance: r.FormValue("normal_balance"),
		Description:   r.FormValue("description"),
		IsActive:      r.FormValue("is_active") == "on",
		IsSystem:      existing.IsSystem,
	}

	errors := validateAccount(a)
	if len(errors) > 0 {
		h.render(w, r, "templates/accounts/form.html", "Edit Account", accountFormData{
			Account: a,
			Errors:  errors,
			IsEdit:  true,
		})
		return
	}

	if err := model.UpdateAccount(h.DB, a); err != nil {
		errors["code"] = "Account code already exists"
		h.render(w, r, "templates/accounts/form.html", "Edit Account", accountFormData{
			Account: a,
			Errors:  errors,
			IsEdit:  true,
		})
		return
	}

	h.setFlash(w, "Account updated successfully")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	account, err := model.GetAccount(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if account.IsSystem {
		http.Error(w, "Cannot delete system account", http.StatusForbidden)
		return
	}

	if err := model.DeleteAccount(h.DB, id); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// For HTMX requests, return empty (row removed)
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "Account deleted successfully")
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func validateAccount(a *model.Account) map[string]string {
	errors := make(map[string]string)
	if a.Code == "" {
		errors["code"] = "Code is required"
	}
	if a.Name == "" {
		errors["name"] = "Name is required"
	}
	if a.AccountType == "" {
		errors["account_type"] = "Account type is required"
	}
	if a.NormalBalance == "" {
		errors["normal_balance"] = "Normal balance is required"
	}
	return errors
}
