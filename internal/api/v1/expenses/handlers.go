// Package expenses implements the /api/v1/expenses CRUD endpoints.
package expenses

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// Handler handles /api/v1/expenses endpoints.
type Handler struct {
	DB *sql.DB
}

// accountRef is a compact account reference returned in responses.
type accountRef struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// expenseEntry is the canonical JSON representation of an expense record.
type expenseEntry struct {
	ID             int         `json:"id"`
	Reference      string      `json:"reference"`
	EntryDate      string      `json:"entry_date"`
	Description    string      `json:"description"`
	Amount         string      `json:"amount"` // IDR as integer string
	ExpenseAccount *accountRef `json:"expense_account,omitempty"`
	PaymentAccount *accountRef `json:"payment_account,omitempty"`
	CreatedAt      string      `json:"created_at"`
}

// expenseInput is the JSON request body for Create and Update.
type expenseInput struct {
	EntryDate      string `json:"entry_date"`
	Description    string `json:"description"`
	Amount         string `json:"amount"` // IDR as integer string
	ExpenseAccount int    `json:"expense_account"`
	PaymentAccount int    `json:"payment_account"`
}

// toExpenseEntry converts a JournalEntry to the API response shape.
// For list entries (no Lines loaded), account refs will be nil.
func toExpenseEntry(je *model.JournalEntry) expenseEntry {
	e := expenseEntry{
		ID:          je.ID,
		Reference:   je.Reference,
		EntryDate:   je.EntryDate,
		Description: je.Description,
		Amount:      strconv.Itoa(je.TotalDebit),
		CreatedAt:   je.CreatedAt,
	}
	// Lines are only populated by GetJournalEntry (not ListJournalEntries).
	for _, l := range je.Lines {
		if l.Debit > 0 {
			// Expense account: debited when expense is recorded
			e.ExpenseAccount = &accountRef{ID: l.AccountID, Code: l.AccountCode, Name: l.AccountName}
		}
		if l.Credit > 0 {
			// Payment account: asset that pays for the expense (credit decreases asset)
			e.PaymentAccount = &accountRef{ID: l.AccountID, Code: l.AccountCode, Name: l.AccountName}
		}
	}
	return e
}

func validateExpenseInput(inp *expenseInput) map[string]string {
	fields := make(map[string]string)
	if inp.EntryDate == "" {
		fields["entry_date"] = "required"
	}
	if inp.Description == "" {
		fields["description"] = "required"
	}
	if inp.Amount == "" {
		fields["amount"] = "required"
	} else {
		amt, err := strconv.Atoi(inp.Amount)
		if err != nil || amt <= 0 {
			fields["amount"] = "must be a positive integer"
		}
	}
	if inp.ExpenseAccount == 0 {
		fields["expense_account"] = "required"
	}
	if inp.PaymentAccount == 0 {
		fields["payment_account"] = "required"
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// extractExpenseShape extracts amount + expense + payment account IDs from journal lines.
func extractExpenseShape(je *model.JournalEntry) (amount, expenseAccount, paymentAccount int) {
	for _, l := range je.Lines {
		if l.Debit > 0 {
			expenseAccount = l.AccountID
			amount = l.Debit
		}
		if l.Credit > 0 {
			paymentAccount = l.AccountID
		}
	}
	return
}

// List handles GET /api/v1/expenses
// Query params: ?from=, ?to=, ?search=, ?page=, ?per_page=
// Auth: any authenticated user.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f := model.JournalFilter{
		SourceType: model.SourceExpense,
		DateFrom:   r.URL.Query().Get("from"),
		DateTo:     r.URL.Query().Get("to"),
		Search:     r.URL.Query().Get("search"),
	}

	entries, err := model.ListJournalEntries(h.DB, f)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list expense entries", nil)
		return
	}

	page := v1.ParsePage(r)
	total := len(entries)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}

	result := make([]expenseEntry, 0, end-start)
	for _, je := range entries[start:end] {
		result = append(result, toExpenseEntry(&je))
	}

	v1.WriteList(w, http.StatusOK, result, page, total)
}

// Get handles GET /api/v1/expenses/{id}
// Auth: any authenticated user.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}

	je, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}

	if je.SourceType != model.SourceExpense {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": toExpenseEntry(je)})
}

// Create handles POST /api/v1/expenses
// Requires: CapExpensesManage. Idempotency-Key supported.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapExpensesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "expenses.manage capability required", nil)
		return
	}

	var inp expenseInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	if fields := validateExpenseInput(&inp); fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	amount, _ := strconv.Atoi(inp.Amount)

	user := auth.UserFromContext(r.Context())
	je := &model.JournalEntry{
		EntryDate:   inp.EntryDate,
		Description: inp.Description,
		SourceType:  model.SourceExpense,
		IsPosted:    true,
		CreatedBy:   user.ID,
	}

	// Debit expense account (increases expense), credit asset (decreases cash/bank)
	lines := []model.JournalLine{
		{AccountID: inp.ExpenseAccount, Debit: amount, Credit: 0},
		{AccountID: inp.PaymentAccount, Debit: 0, Credit: amount},
	}

	entryID, err := model.CreateJournalEntry(h.DB, je, lines)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create expense entry", nil)
		return
	}

	created, err := model.GetJournalEntry(h.DB, entryID)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve created expense entry", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "expense.create",
		TargetType:  "expense",
		TargetID:    int64(entryID),
		TargetLabel: inp.Description,
		Metadata: map[string]any{
			"after": map[string]any{
				"entry_date":      inp.EntryDate,
				"description":     inp.Description,
				"amount":          amount,
				"expense_account": inp.ExpenseAccount,
				"payment_account": inp.PaymentAccount,
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": toExpenseEntry(created)})
}

// Update handles PUT /api/v1/expenses/{id}
// Requires: CapExpensesManage. Idempotency-Key supported.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapExpensesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "expenses.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}

	existing, err := model.GetJournalEntry(h.DB, id)
	if err != nil || existing.SourceType != model.SourceExpense {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}

	var inp expenseInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	if fields := validateExpenseInput(&inp); fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	amount, _ := strconv.Atoi(inp.Amount)

	je := &model.JournalEntry{
		ID:          id,
		EntryDate:   inp.EntryDate,
		Description: inp.Description,
		SourceType:  model.SourceExpense,
		IsPosted:    true,
	}

	lines := []model.JournalLine{
		{AccountID: inp.ExpenseAccount, Debit: amount, Credit: 0},
		{AccountID: inp.PaymentAccount, Debit: 0, Credit: amount},
	}

	if err := model.UpdateJournalEntry(h.DB, je, lines); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update expense entry", nil)
		return
	}

	updated, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve updated expense entry", nil)
		return
	}

	oldAmount, oldExpenseAcct, oldPaymentAcct := extractExpenseShape(existing)
	oldFields := map[string]any{
		"entry_date":      existing.EntryDate,
		"description":     existing.Description,
		"amount":          oldAmount,
		"expense_account": oldExpenseAcct,
		"payment_account": oldPaymentAcct,
	}
	newFields := map[string]any{
		"entry_date":      inp.EntryDate,
		"description":     inp.Description,
		"amount":          amount,
		"expense_account": inp.ExpenseAccount,
		"payment_account": inp.PaymentAccount,
	}
	if metadata := audit.Diff(oldFields, newFields,
		[]string{"entry_date", "description", "amount", "expense_account", "payment_account"}); metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "expense.update",
			TargetType:  "expense",
			TargetID:    int64(id),
			TargetLabel: inp.Description,
			Metadata:    metadata,
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": toExpenseEntry(updated)})
}

// Delete handles DELETE /api/v1/expenses/{id}
// Requires: CapExpensesManage. Returns 204.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapExpensesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "expenses.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}

	existing, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}
	if existing.SourceType != model.SourceExpense {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "expense entry not found", nil)
		return
	}

	if err := model.DeleteJournalEntryBySource(h.DB, id, model.SourceExpense); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	amount, expenseAcct, paymentAcct := extractExpenseShape(existing)
	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "expense.delete",
		TargetType:  "expense",
		TargetID:    int64(id),
		TargetLabel: existing.Description,
		Metadata: map[string]any{
			"before": map[string]any{
				"entry_date":      existing.EntryDate,
				"description":     existing.Description,
				"amount":          amount,
				"expense_account": expenseAcct,
				"payment_account": paymentAcct,
			},
		},
	})

	w.WriteHeader(http.StatusNoContent)
}
