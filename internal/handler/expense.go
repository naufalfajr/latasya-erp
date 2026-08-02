package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
)

type expenseFormData struct {
	Entry           *model.JournalEntry
	Amount          int
	ExpenseAccount  int
	PaymentAccount  int
	VehicleID       int
	ExpenseAccounts []model.Account
	PaymentAccounts []model.Account
	Vehicles        []model.Vehicle
	Errors          map[string]string
	IsEdit          bool
}

func (h *Handler) ListExpenses(w http.ResponseWriter, r *http.Request) {
	filter := journal.Filter{SourceType: model.SourceExpense, DateFrom: r.URL.Query().Get("from"),
		DateTo: r.URL.Query().Get("to"), Search: r.URL.Query().Get("search")}
	page := parsePage(r)
	filter.Limit, filter.Offset = listPageSize, (page-1)*listPageSize
	result, err := h.Journals.List(r.Context(), filter)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	pg := newPagination(page, result.Total)
	h.render(w, r, "templates/expenses/index.html", "Expenses", map[string]any{
		"Entries": result.Entries, "Pagination": newPageNav(pg, map[string]string{"from": filter.DateFrom, "to": filter.DateTo, "search": filter.Search}),
	})
}

func (h *Handler) NewExpense(w http.ResponseWriter, r *http.Request) {
	form, err := h.newExpenseFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/expenses/form.html", "Record Expense", form)
}

func (h *Handler) CreateExpense(w http.ResponseWriter, r *http.Request) {
	draft := expenseDraftFromForm(r)
	created, err := h.Journals.CreateExpense(r.Context(), expenseActor(r), draft)
	if err != nil {
		h.renderExpenseError(w, r, "Record Expense", draft, false, err)
		return
	}
	h.setFlash(w, "Expense recorded successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/journals/%d", created.ID), http.StatusSeeOther)
}

func (h *Handler) EditExpense(w http.ResponseWriter, r *http.Request) {
	entry, ok := h.expenseEntry(w, r)
	if !ok {
		return
	}
	form, err := h.newExpenseFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	form.Entry, form.VehicleID, form.IsEdit = entry, entry.VehicleID, true
	form.Amount, form.ExpenseAccount, form.PaymentAccount = extractExpenseShape(entry)
	h.render(w, r, "templates/expenses/form.html", "Edit Expense", form)
}

func (h *Handler) UpdateExpense(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if entry, err := h.Journals.Get(r.Context(), id); err != nil || entry.SourceType != model.SourceExpense {
		http.NotFound(w, r)
		return
	}
	draft := expenseDraftFromForm(r)
	if _, err := h.Journals.UpdateExpense(r.Context(), expenseActor(r), id, draft); err != nil {
		h.renderExpenseError(w, r, "Edit Expense", draft, true, err)
		return
	}
	h.setFlash(w, "Expense updated successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/journals/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteExpense(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.Journals.DeleteExpense(r.Context(), expenseActor(r), id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, h.BasePath+"/expenses", http.StatusSeeOther)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.setFlash(w, "Expense deleted successfully")
	http.Redirect(w, r, h.BasePath+"/expenses", http.StatusSeeOther)
}

func expenseDraftFromForm(r *http.Request) journal.ExpenseDraft {
	expense, _ := strconv.Atoi(r.FormValue("expense_account"))
	payment, _ := strconv.Atoi(r.FormValue("payment_account"))
	return journal.ExpenseDraft{EntryDate: r.FormValue("entry_date"), Description: r.FormValue("description"), Amount: parseIDR(r.FormValue("amount")),
		ExpenseAccount: expense, PaymentAccount: payment, VehicleID: parseOptionalInt(r.FormValue("vehicle_id"))}
}

func (h *Handler) newExpenseFormData(r *http.Request) (expenseFormData, error) {
	expense, err := h.Journals.Options(r.Context(), model.AccountTypeExpense, false)
	if err != nil {
		return expenseFormData{}, err
	}
	payment, err := h.Journals.Options(r.Context(), model.AccountTypeAsset, true)
	if err != nil {
		return expenseFormData{}, err
	}
	return expenseFormData{Entry: &model.JournalEntry{}, ExpenseAccounts: expense.Accounts, PaymentAccounts: payment.Accounts,
		Vehicles: payment.Vehicles, Errors: map[string]string{}}, nil
}

func (h *Handler) renderExpenseError(w http.ResponseWriter, r *http.Request, title string, draft journal.ExpenseDraft, edit bool, err error) {
	form, optionErr := h.newExpenseFormData(r)
	if optionErr != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	form.Entry = &model.JournalEntry{ID: parsePathID(r), EntryDate: draft.EntryDate, Description: draft.Description,
		SourceType: model.SourceExpense, VehicleID: draft.VehicleID, IsPosted: true}
	form.Amount, form.ExpenseAccount, form.PaymentAccount, form.VehicleID = draft.Amount, draft.ExpenseAccount, draft.PaymentAccount, draft.VehicleID
	form.Errors, form.IsEdit = moduleFields(err), edit
	if len(form.Errors) == 0 {
		form.Errors["general"] = err.Error()
	}
	h.render(w, r, "templates/expenses/form.html", title, form)
}

func (h *Handler) expenseEntry(w http.ResponseWriter, r *http.Request) (*model.JournalEntry, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	entry, err := h.Journals.Get(r.Context(), id)
	if err != nil || entry.SourceType != model.SourceExpense {
		http.NotFound(w, r)
		return nil, false
	}
	return entry, true
}

func expenseActor(r *http.Request) journal.Actor {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		return journal.Actor{}
	}
	return journal.Actor{UserID: user.ID, CanManageExpenses: user.HasCapability(model.CapExpensesManage)}
}

func extractExpenseShape(entry *model.JournalEntry) (amount, expenseAccount, paymentAccount int) {
	for _, line := range entry.Lines {
		if line.Debit > 0 {
			expenseAccount, amount = line.AccountID, line.Debit
		}
		if line.Credit > 0 {
			paymentAccount = line.AccountID
		}
	}
	return
}
