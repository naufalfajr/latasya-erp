// Package accounts implements the /api/v1/accounts CRUD endpoints.
package accounts

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/account"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Accounts *account.Module }

type accountInput struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	AccountType   string `json:"account_type"`
	NormalBalance string `json:"normal_balance"`
	Description   string `json:"description"`
	IsActive      *bool  `json:"is_active"`
	IsCash        *bool  `json:"is_cash"`
}

func actor(r *http.Request) account.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return account.Actor{}
	}
	return account.Actor{UserID: u.ID, CanManage: v1.HasEffectiveCapability(r.Context(), model.CapAccountsManage)}
}

func writeModuleError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	if errors.Is(err, account.ErrForbidden) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}
	if errors.Is(err, account.ErrNotFound) {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "account not found", nil)
		return
	}
	var validation *account.ValidationError
	if errors.As(err, &validation) {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", validation.Fields)
		return
	}
	var conflict *account.ConflictError
	if errors.As(err, &conflict) {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, conflict.Message, nil)
		return
	}
	v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, fallback, nil)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	result, err := h.Accounts.List(r.Context(), account.Filter{Type: r.URL.Query().Get("type"), Search: r.URL.Query().Get("search"), Limit: page.PerPage, Offset: page.Offset()})
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list accounts", nil)
		return
	}
	v1.WriteList(w, http.StatusOK, result.Accounts, page, result.Total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid account id", nil)
		return
	}
	a, err := h.Accounts.Get(r.Context(), id)
	if err != nil {
		writeModuleError(w, r, err, "failed to get account")
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": a})
}

func createDraft(inp accountInput) account.Draft {
	isActive := true
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}
	isCash := false
	if inp.IsCash != nil {
		isCash = *inp.IsCash
	}
	return account.Draft{Code: inp.Code, Name: inp.Name, AccountType: inp.AccountType, NormalBalance: inp.NormalBalance, Description: inp.Description, IsActive: isActive, IsCash: isCash}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapAccountsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}
	var inp accountInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	created, err := h.Accounts.Create(r.Context(), actor(r), createDraft(inp))
	if err != nil {
		writeModuleError(w, r, err, "failed to create account")
		return
	}
	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": created})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapAccountsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid account id", nil)
		return
	}
	existing, err := h.Accounts.Get(r.Context(), id)
	if err != nil {
		writeModuleError(w, r, err, "failed to get account")
		return
	}
	var inp accountInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	isActive, isCash := existing.IsActive, existing.IsCash
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}
	if inp.IsCash != nil {
		isCash = *inp.IsCash
	}
	draft := account.Draft{Code: inp.Code, Name: inp.Name, AccountType: inp.AccountType, NormalBalance: inp.NormalBalance, Description: inp.Description, IsActive: isActive, IsCash: isCash}
	updated, err := h.Accounts.Update(r.Context(), actor(r), id, draft)
	if err != nil {
		writeModuleError(w, r, err, "failed to update account")
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapAccountsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid account id", nil)
		return
	}
	if _, err := h.Accounts.Delete(r.Context(), actor(r), id); err != nil {
		writeModuleError(w, r, err, "failed to delete account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/accounts", h.List)
	mux.HandleFunc("GET /api/v1/accounts/{id}", h.Get)
	mux.HandleFunc("POST /api/v1/accounts", h.Create)
	mux.HandleFunc("PUT /api/v1/accounts/{id}", h.Update)
	mux.HandleFunc("DELETE /api/v1/accounts/{id}", h.Delete)
}
