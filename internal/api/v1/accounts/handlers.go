// Package accounts implements the /api/v1/accounts CRUD endpoints.
package accounts

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

// Handler handles /api/v1/accounts endpoints.
type Handler struct {
	DB *sql.DB
}

// List handles GET /api/v1/accounts
// Query params: ?type=, ?search=, ?page=, ?per_page=
// Auth: any authenticated user.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	filter := model.AccountFilter{
		Type:   r.URL.Query().Get("type"),
		Search: r.URL.Query().Get("search"),
	}

	accounts, err := model.ListAccounts(h.DB, filter)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list accounts", nil)
		return
	}
	if accounts == nil {
		accounts = []model.Account{}
	}

	page := v1.ParsePage(r)
	total := len(accounts)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}

	v1.WriteList(w, http.StatusOK, accounts[start:end], page, total)
}

// Get handles GET /api/v1/accounts/{id}
// Auth: any authenticated user.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid account id", nil)
		return
	}

	account, err := model.GetAccount(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "account not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "account not found", nil)
		return
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": account})
}

// accountInput is the request body for Create and Update.
type accountInput struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	AccountType   string `json:"account_type"`
	NormalBalance string `json:"normal_balance"`
	Description   string `json:"description"`
	IsActive      *bool  `json:"is_active"`
}

var validAccountTypes = map[string]bool{
	"asset": true, "liability": true, "equity": true,
	"revenue": true, "expense": true,
}

var validNormalBalances = map[string]bool{
	"debit": true, "credit": true,
}

func validateInput(inp *accountInput) map[string]string {
	fields := make(map[string]string)
	if strings.TrimSpace(inp.Code) == "" {
		fields["code"] = "required"
	}
	if strings.TrimSpace(inp.Name) == "" {
		fields["name"] = "required"
	}
	if inp.AccountType == "" {
		fields["account_type"] = "required"
	} else if !validAccountTypes[inp.AccountType] {
		fields["account_type"] = "must be one of: asset, liability, equity, revenue, expense"
	}
	if inp.NormalBalance == "" {
		fields["normal_balance"] = "required"
	} else if !validNormalBalances[inp.NormalBalance] {
		fields["normal_balance"] = "must be one of: debit, credit"
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// Create handles POST /api/v1/accounts
// Requires: CapAccountsManage
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

	if fields := validateInput(&inp); fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	isActive := true
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}

	a := &model.Account{
		Code:          inp.Code,
		Name:          inp.Name,
		AccountType:   inp.AccountType,
		NormalBalance: inp.NormalBalance,
		Description:   inp.Description,
		IsActive:      isActive,
	}

	if err := model.CreateAccount(h.DB, a); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "account code already exists", nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create account", nil)
		return
	}

	var createdID int64
	h.DB.QueryRow("SELECT last_insert_rowid()").Scan(&createdID) //nolint:errcheck

	created, err := model.GetAccount(h.DB, int(createdID))
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve created account", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "account.create",
		TargetType:  "account",
		TargetID:    createdID,
		TargetLabel: created.Code,
		Metadata: map[string]any{
			"after": map[string]any{
				"code":           created.Code,
				"name":           created.Name,
				"account_type":   created.AccountType,
				"normal_balance": created.NormalBalance,
				"is_active":      created.IsActive,
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": created})
}

// Update handles PUT /api/v1/accounts/{id}
// Requires: CapAccountsManage
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

	existing, err := model.GetAccount(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "account not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "account not found", nil)
		return
	}

	var inp accountInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	if fields := validateInput(&inp); fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	isActive := existing.IsActive
	if inp.IsActive != nil {
		isActive = *inp.IsActive
	}

	a := &model.Account{
		ID:            id,
		Code:          inp.Code,
		Name:          inp.Name,
		AccountType:   inp.AccountType,
		NormalBalance: inp.NormalBalance,
		Description:   inp.Description,
		IsActive:      isActive,
		IsSystem:      existing.IsSystem,
		ParentID:      existing.ParentID,
	}

	if err := model.UpdateAccount(h.DB, a); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update account", nil)
		return
	}

	updated, err := model.GetAccount(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve updated account", nil)
		return
	}

	oldFields := map[string]any{
		"code":           existing.Code,
		"name":           existing.Name,
		"account_type":   existing.AccountType,
		"normal_balance": existing.NormalBalance,
		"description":    existing.Description,
		"is_active":      existing.IsActive,
	}
	newFields := map[string]any{
		"code":           updated.Code,
		"name":           updated.Name,
		"account_type":   updated.AccountType,
		"normal_balance": updated.NormalBalance,
		"description":    updated.Description,
		"is_active":      updated.IsActive,
	}
	if metadata := audit.Diff(oldFields, newFields,
		[]string{"code", "name", "account_type", "normal_balance", "description", "is_active"}); metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "account.update",
			TargetType:  "account",
			TargetID:    int64(id),
			TargetLabel: existing.Code,
			Metadata:    metadata,
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}

// Delete handles DELETE /api/v1/accounts/{id}
// Requires: CapAccountsManage
// Returns 409 for system accounts or accounts with linked transactions.
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

	account, err := model.GetAccount(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "account not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "account not found", nil)
		return
	}

	if account.IsSystem {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "cannot delete system account", nil)
		return
	}

	if err := model.DeleteAccount(h.DB, id); err != nil {
		if strings.Contains(err.Error(), "linked transaction") {
			v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, "account has linked transactions and cannot be deleted", nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to delete account", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "account.delete",
		TargetType:  "account",
		TargetID:    int64(id),
		TargetLabel: account.Code,
		Metadata: map[string]any{
			"before": map[string]any{
				"code":           account.Code,
				"name":           account.Name,
				"account_type":   account.AccountType,
				"normal_balance": account.NormalBalance,
			},
		},
	})

	w.WriteHeader(http.StatusNoContent)
}
