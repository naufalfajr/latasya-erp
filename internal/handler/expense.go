package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type expenseFormData struct {
	Entry           *model.JournalEntry
	Amount          int
	ExpenseAccount  int
	PaymentAccount  int
	ExpenseAccounts []model.Account
	PaymentAccounts []model.Account
	Errors          map[string]string
	IsEdit          bool
}

func (h *Handler) ListExpenses(w http.ResponseWriter, r *http.Request) {
	f := model.JournalFilter{
		SourceType: model.SourceExpense,
		DateFrom:   r.URL.Query().Get("from"),
		DateTo:     r.URL.Query().Get("to"),
		Search:     r.URL.Query().Get("search"),
	}

	entries, err := model.ListJournalEntries(h.DB, f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/expenses/index.html", "Expenses", entries)
}

func (h *Handler) NewExpense(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/expenses/form.html", "Record Expense", h.newExpenseFormData())
}

func (h *Handler) CreateExpense(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	amount := parseIDR(r.FormValue("amount"))
	expenseAccountID, _ := strconv.Atoi(r.FormValue("expense_account"))
	paymentAccountID, _ := strconv.Atoi(r.FormValue("payment_account"))

	je := &model.JournalEntry{
		EntryDate:   r.FormValue("entry_date"),
		Description: r.FormValue("description"),
		SourceType:  "expense",
		IsPosted:    true,
		CreatedBy:   user.ID,
	}

	errors := make(map[string]string)
	if je.EntryDate == "" {
		errors["entry_date"] = "Date is required"
	}
	if je.Description == "" {
		errors["description"] = "Description is required"
	}
	if amount <= 0 {
		errors["amount"] = "Amount must be greater than 0"
	}
	if expenseAccountID == 0 {
		errors["expense_account"] = "Expense account is required"
	}
	if paymentAccountID == 0 {
		errors["payment_account"] = "Payment account is required"
	}

	if len(errors) > 0 {
		fd := h.newExpenseFormData()
		fd.Entry = je
		fd.Amount = amount
		fd.ExpenseAccount = expenseAccountID
		fd.PaymentAccount = paymentAccountID
		fd.Errors = errors
		h.render(w, r, "templates/expenses/form.html", "Record Expense", fd)
		return
	}

	// Debit expense (increases expense), Credit asset (decreases cash/bank)
	lines := []model.JournalLine{
		{AccountID: expenseAccountID, Debit: amount, Credit: 0},
		{AccountID: paymentAccountID, Debit: 0, Credit: amount},
	}

	entryID, err := model.CreateJournalEntry(h.DB, je, lines)
	if err != nil {
		fd := h.newExpenseFormData()
		fd.Entry = je
		fd.Amount = amount
		fd.ExpenseAccount = expenseAccountID
		fd.PaymentAccount = paymentAccountID
		fd.Errors = map[string]string{"general": err.Error()}
		h.render(w, r, "templates/expenses/form.html", "Record Expense", fd)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "expense.create",
		TargetType:  "expense",
		TargetID:    int64(entryID),
		TargetLabel: je.Description,
		Metadata: map[string]any{
			"after": map[string]any{
				"entry_date":      je.EntryDate,
				"description":     je.Description,
				"amount":          amount,
				"expense_account": expenseAccountID,
				"payment_account": paymentAccountID,
			},
		},
	})

	h.setFlash(w, "Expense recorded successfully")
	http.Redirect(w, r, fmt.Sprintf("/journals/%d", entryID), http.StatusSeeOther)
}

func (h *Handler) EditExpense(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	je, err := model.GetJournalEntry(h.DB, id)
	if err != nil || je.SourceType != model.SourceExpense {
		http.NotFound(w, r)
		return
	}

	fd := h.newExpenseFormData()
	fd.Entry = je
	fd.IsEdit = true

	for _, l := range je.Lines {
		if l.Debit > 0 {
			fd.ExpenseAccount = l.AccountID
			fd.Amount = l.Debit
		}
		if l.Credit > 0 {
			fd.PaymentAccount = l.AccountID
		}
	}

	h.render(w, r, "templates/expenses/form.html", "Edit Expense", fd)
}

func (h *Handler) UpdateExpense(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	oldJE, err := model.GetJournalEntry(h.DB, id)
	if err != nil || oldJE.SourceType != model.SourceExpense {
		http.NotFound(w, r)
		return
	}

	user := auth.UserFromContext(r.Context())
	amount := parseIDR(r.FormValue("amount"))
	expenseAccountID, _ := strconv.Atoi(r.FormValue("expense_account"))
	paymentAccountID, _ := strconv.Atoi(r.FormValue("payment_account"))

	je := &model.JournalEntry{
		ID:          id,
		EntryDate:   r.FormValue("entry_date"),
		Description: r.FormValue("description"),
		SourceType:  "expense",
		IsPosted:    true,
		CreatedBy:   user.ID,
	}

	errors := make(map[string]string)
	if je.EntryDate == "" {
		errors["entry_date"] = "Date is required"
	}
	if je.Description == "" {
		errors["description"] = "Description is required"
	}
	if amount <= 0 {
		errors["amount"] = "Amount must be greater than 0"
	}
	if expenseAccountID == 0 {
		errors["expense_account"] = "Expense account is required"
	}
	if paymentAccountID == 0 {
		errors["payment_account"] = "Payment account is required"
	}

	if len(errors) > 0 {
		fd := h.newExpenseFormData()
		fd.Entry = je
		fd.Amount = amount
		fd.ExpenseAccount = expenseAccountID
		fd.PaymentAccount = paymentAccountID
		fd.Errors = errors
		fd.IsEdit = true
		h.render(w, r, "templates/expenses/form.html", "Edit Expense", fd)
		return
	}

	lines := []model.JournalLine{
		{AccountID: expenseAccountID, Debit: amount, Credit: 0},
		{AccountID: paymentAccountID, Debit: 0, Credit: amount},
	}

	if err := model.UpdateJournalEntry(h.DB, je, lines); err != nil {
		fd := h.newExpenseFormData()
		fd.Entry = je
		fd.Amount = amount
		fd.ExpenseAccount = expenseAccountID
		fd.PaymentAccount = paymentAccountID
		fd.Errors = map[string]string{"general": err.Error()}
		fd.IsEdit = true
		h.render(w, r, "templates/expenses/form.html", "Edit Expense", fd)
		return
	}

	oldAmount, oldExpenseAcct, oldPaymentAcct := extractExpenseShape(oldJE)
	oldFields := map[string]any{
		"entry_date":      oldJE.EntryDate,
		"description":     oldJE.Description,
		"amount":          oldAmount,
		"expense_account": oldExpenseAcct,
		"payment_account": oldPaymentAcct,
	}
	newFields := map[string]any{
		"entry_date":      je.EntryDate,
		"description":     je.Description,
		"amount":          amount,
		"expense_account": expenseAccountID,
		"payment_account": paymentAccountID,
	}
	metadata := audit.Diff(oldFields, newFields,
		[]string{"entry_date", "description", "amount", "expense_account", "payment_account"})
	if metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "expense.update",
			TargetType:  "expense",
			TargetID:    int64(id),
			TargetLabel: je.Description,
			Metadata:    metadata,
		})
	}

	h.setFlash(w, "Expense updated successfully")
	http.Redirect(w, r, fmt.Sprintf("/journals/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteExpense(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	existing, _ := model.GetJournalEntry(h.DB, id)

	if err := model.DeleteJournalEntryBySource(h.DB, id, model.SourceExpense); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, "/expenses", http.StatusSeeOther)
		return
	}

	if existing != nil {
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
	}

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "Expense deleted successfully")
	http.Redirect(w, r, "/expenses", http.StatusSeeOther)
}

func (h *Handler) newExpenseFormData() expenseFormData {
	active := true
	expenseAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: model.AccountTypeExpense, IsActive: &active})
	paymentAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "asset", IsActive: &active})

	return expenseFormData{
		Entry:           &model.JournalEntry{},
		ExpenseAccounts: expenseAccounts,
		PaymentAccounts: paymentAccounts,
		Errors:          make(map[string]string),
	}
}

// extractExpenseShape pulls "amount + expense + payment account" out of the
// journal lines that expense.create wrote (debit on expense, credit on the
// asset paying it out).
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
