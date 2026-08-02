// Package expenses implements the /api/v1/expenses CRUD endpoints.
package expenses

import (
	"errors"
	"net/http"
	"strconv"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Journals *journal.Module }

type accountRef struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type expenseEntry struct {
	ID             int         `json:"id"`
	Reference      string      `json:"reference"`
	EntryDate      string      `json:"entry_date"`
	Description    string      `json:"description"`
	Amount         string      `json:"amount"`
	ExpenseAccount *accountRef `json:"expense_account,omitempty"`
	PaymentAccount *accountRef `json:"payment_account,omitempty"`
	CreatedAt      string      `json:"created_at"`
}

type expenseInput struct {
	EntryDate      string `json:"entry_date"`
	Description    string `json:"description"`
	Amount         string `json:"amount"`
	ExpenseAccount int    `json:"expense_account"`
	PaymentAccount int    `json:"payment_account"`
}

func toExpenseEntry(entry *model.JournalEntry) expenseEntry {
	result := expenseEntry{ID: entry.ID, Reference: entry.Reference, EntryDate: entry.EntryDate,
		Description: entry.Description, Amount: strconv.Itoa(entry.TotalDebit), CreatedAt: entry.CreatedAt}
	for _, line := range entry.Lines {
		if line.Debit > 0 {
			result.ExpenseAccount = &accountRef{ID: line.AccountID, Code: line.AccountCode, Name: line.AccountName}
		}
		if line.Credit > 0 {
			result.PaymentAccount = &accountRef{ID: line.AccountID, Code: line.AccountCode, Name: line.AccountName}
		}
	}
	return result
}

func actor(r *http.Request) journal.Actor {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		return journal.Actor{}
	}
	return journal.Actor{UserID: user.ID, CanManageExpenses: v1.HasEffectiveCapability(r.Context(), model.CapExpensesManage)}
}

func draft(input expenseInput) (journal.ExpenseDraft, map[string]string) {
	amount := 0
	var err error
	if input.Amount != "" {
		amount, err = strconv.Atoi(input.Amount)
	}
	if err != nil {
		return journal.ExpenseDraft{}, map[string]string{"amount": "must be a positive integer"}
	}
	return journal.ExpenseDraft{EntryDate: input.EntryDate, Description: input.Description, Amount: amount,
		ExpenseAccount: input.ExpenseAccount, PaymentAccount: input.PaymentAccount}, nil
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	result, err := h.Journals.List(r.Context(), journal.Filter{SourceType: model.SourceExpense,
		DateFrom: r.URL.Query().Get("from"), DateTo: r.URL.Query().Get("to"), Search: r.URL.Query().Get("search"),
		Limit: page.PerPage, Offset: page.Offset()})
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list expense entries", nil)
		return
	}
	entries := make([]expenseEntry, 0, len(result.Entries))
	for i := range result.Entries {
		entries = append(entries, toExpenseEntry(&result.Entries[i]))
	}
	v1.WriteList(w, http.StatusOK, entries, page, result.Total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	entry, ok := h.get(w, r)
	if !ok {
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": toExpenseEntry(entry)})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !requireManage(w, r) {
		return
	}
	var input expenseInput
	if err := v1.DecodeJSON(w, r, &input); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	command, fields := draft(input)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	created, err := h.Journals.CreateExpense(r.Context(), actor(r), command)
	if !writeModuleError(w, r, err) {
		return
	}
	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": toExpenseEntry(created)})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !requireManage(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}
	if entry, err := h.Journals.Get(r.Context(), id); err != nil || entry.SourceType != model.SourceExpense {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}
	var input expenseInput
	if err := v1.DecodeJSON(w, r, &input); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	command, fields := draft(input)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	updated, err := h.Journals.UpdateExpense(r.Context(), actor(r), id, command)
	if !writeModuleError(w, r, err) {
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": toExpenseEntry(updated)})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !requireManage(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}
	if entry, err := h.Journals.Get(r.Context(), id); err != nil || entry.SourceType != model.SourceExpense {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}
	_, err = h.Journals.DeleteExpense(r.Context(), actor(r), id)
	if !writeModuleError(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requireManage(w http.ResponseWriter, r *http.Request) bool {
	if v1.HasEffectiveCapability(r.Context(), model.CapExpensesManage) {
		return true
	}
	v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "expenses.manage capability required", nil)
	return false
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) (*model.JournalEntry, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return nil, false
	}
	entry, err := h.Journals.Get(r.Context(), id)
	if err != nil || entry.SourceType != model.SourceExpense {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return nil, false
	}
	return entry, true
}

func writeModuleError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return true
	}
	var validation *journal.ValidationError
	var conflict *journal.ConflictError
	switch {
	case errors.Is(err, journal.ErrForbidden):
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "expenses.manage capability required", nil)
	case errors.Is(err, journal.ErrNotFound):
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
	case errors.As(err, &validation):
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, validation.Error(), validation.Fields)
	case errors.As(err, &conflict):
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, conflict.Error(), nil)
	default:
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "expense operation failed", nil)
	}
	return false
}
