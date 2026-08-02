package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type accountPageData struct {
	Accounts   []model.Account
	Filter     string
	Search     string
	TypeCounts map[string]int
}

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	active := true
	result, err := h.Accounts.List(r.Context(), account.Filter{Type: r.URL.Query().Get("type"), IsActive: &active, Search: r.URL.Query().Get("search")})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	counts, err := h.Accounts.TypeCounts(r.Context(), true)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	data := accountPageData{Accounts: result.Accounts, Filter: r.URL.Query().Get("type"), Search: r.URL.Query().Get("search"), TypeCounts: counts}
	if r.Header.Get("HX-Request") == "true" {
		h.renderFragment(w, r, "templates/accounts/index.html", "account-table", data)
		return
	}
	h.render(w, r, "templates/accounts/index.html", "Chart of Accounts", data)
}

type accountFormData struct {
	Account *model.Account
	Errors  map[string]string
	IsEdit  bool
}

func (h *Handler) NewAccount(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/accounts/form.html", "New Account", accountFormData{Account: &model.Account{IsActive: true}})
}

func accountDraft(r *http.Request) account.Draft {
	return account.Draft{Code: r.FormValue("code"), Name: r.FormValue("name"), AccountType: r.FormValue("account_type"), NormalBalance: r.FormValue("normal_balance"), Description: r.FormValue("description"), IsActive: r.FormValue("is_active") == "on", IsCash: r.FormValue("is_cash") == "on"}
}

func accountView(id int, d account.Draft) *model.Account {
	return &model.Account{ID: id, Code: d.Code, Name: d.Name, AccountType: d.AccountType, NormalBalance: d.NormalBalance, Description: d.Description, IsActive: d.IsActive, IsCash: d.IsCash}
}

func accountActor(r *http.Request) account.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return account.Actor{}
	}
	return account.Actor{UserID: u.ID, CanManage: u.HasCapability(model.CapAccountsManage)}
}

func accountFormErrors(err error) map[string]string {
	var validation *account.ValidationError
	if errors.As(err, &validation) {
		fields := map[string]string{}
		for field, message := range validation.Fields {
			if message == "required" {
				message = map[string]string{"code": "Code is required", "name": "Name is required", "account_type": "Account type is required", "normal_balance": "Normal balance is required"}[field]
			}
			fields[field] = message
		}
		return fields
	}
	var conflict *account.ConflictError
	if errors.As(err, &conflict) {
		return map[string]string{"code": "Account code already exists"}
	}
	return nil
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	draft := accountDraft(r)
	_, err := h.Accounts.Create(r.Context(), accountActor(r), draft)
	if err != nil {
		if fields := accountFormErrors(err); fields != nil {
			h.render(w, r, "templates/accounts/form.html", "New Account", accountFormData{Account: accountView(0, draft), Errors: fields})
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.setFlash(w, "Account created successfully")
	http.Redirect(w, r, h.BasePath+"/accounts", http.StatusSeeOther)
}

func (h *Handler) EditAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a, err := h.Accounts.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, "templates/accounts/form.html", "Edit Account", accountFormData{Account: a, IsEdit: true})
}

func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	draft := accountDraft(r)
	_, err = h.Accounts.Update(r.Context(), accountActor(r), id, draft)
	if err != nil {
		if errors.Is(err, account.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if fields := accountFormErrors(err); fields != nil {
			h.render(w, r, "templates/accounts/form.html", "Edit Account", accountFormData{Account: accountView(id, draft), Errors: fields, IsEdit: true})
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.setFlash(w, "Account updated successfully")
	http.Redirect(w, r, h.BasePath+"/accounts", http.StatusSeeOther)
}

func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = h.Accounts.Delete(r.Context(), accountActor(r), id)
	if err != nil {
		if errors.Is(err, account.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		var conflict *account.ConflictError
		if errors.As(err, &conflict) && conflict.Message == "cannot delete system account" {
			http.Error(w, "Cannot delete system account", http.StatusForbidden)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.setFlash(w, "Account deleted successfully")
	http.Redirect(w, r, h.BasePath+"/accounts", http.StatusSeeOther)
}

// RegisterAccountRoutes installs chart-of-accounts HTML endpoints.
func (h *Handler) RegisterAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /accounts", h.ListAccounts)
	mux.HandleFunc("GET /accounts/new", h.NewAccount)
	mux.HandleFunc("POST /accounts", auth.CapabilityOnly(model.CapAccountsManage, h.CreateAccount))
	mux.HandleFunc("GET /accounts/{id}/edit", h.EditAccount)
	mux.HandleFunc("POST /accounts/{id}", auth.CapabilityOnly(model.CapAccountsManage, h.UpdateAccount))
	mux.HandleFunc("DELETE /accounts/{id}", auth.CapabilityOnly(model.CapAccountsManage, h.DeleteAccount))
}
