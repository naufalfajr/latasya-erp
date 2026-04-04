package handler

import (
	"fmt"
	"net/http"
	"strconv"

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
		SourceType: "expense",
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
	if err != nil || je.SourceType != "expense" {
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

	h.setFlash(w, "Expense updated successfully")
	http.Redirect(w, r, fmt.Sprintf("/journals/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteExpense(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	h.DB.Exec("UPDATE journal_entries SET source_type = 'manual' WHERE id = ? AND source_type = 'expense'", id)
	model.DeleteJournalEntry(h.DB, id)

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "Expense deleted successfully")
	http.Redirect(w, r, "/expenses", http.StatusSeeOther)
}

func (h *Handler) newExpenseFormData() expenseFormData {
	active := true
	expenseAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "expense", IsActive: &active})
	paymentAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "asset", IsActive: &active})

	return expenseFormData{
		Entry:           &model.JournalEntry{},
		ExpenseAccounts: expenseAccounts,
		PaymentAccounts: paymentAccounts,
		Errors:          make(map[string]string),
	}
}
