package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type incomeFormData struct {
	Entry          *model.JournalEntry
	Amount         int
	RevenueAccount int
	DepositAccount int
	RevenueAccounts []model.Account
	DepositAccounts []model.Account
	Errors         map[string]string
	IsEdit         bool
}

func (h *Handler) ListIncome(w http.ResponseWriter, r *http.Request) {
	f := model.JournalFilter{
		SourceType: "income",
		DateFrom:   r.URL.Query().Get("from"),
		DateTo:     r.URL.Query().Get("to"),
		Search:     r.URL.Query().Get("search"),
	}

	entries, err := model.ListJournalEntries(h.DB, f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/income/index.html", "Income", entries)
}

func (h *Handler) NewIncome(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "templates/income/form.html", "Record Income", h.newIncomeFormData())
}

func (h *Handler) CreateIncome(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	amount := parseIDR(r.FormValue("amount"))
	revenueAccountID, _ := strconv.Atoi(r.FormValue("revenue_account"))
	depositAccountID, _ := strconv.Atoi(r.FormValue("deposit_account"))

	je := &model.JournalEntry{
		EntryDate:   r.FormValue("entry_date"),
		Description: r.FormValue("description"),
		SourceType:  "income",
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
	if revenueAccountID == 0 {
		errors["revenue_account"] = "Revenue account is required"
	}
	if depositAccountID == 0 {
		errors["deposit_account"] = "Deposit account is required"
	}

	if len(errors) > 0 {
		fd := h.newIncomeFormData()
		fd.Entry = je
		fd.Amount = amount
		fd.RevenueAccount = revenueAccountID
		fd.DepositAccount = depositAccountID
		fd.Errors = errors
		h.render(w, r, "templates/income/form.html", "Record Income", fd)
		return
	}

	lines := []model.JournalLine{
		{AccountID: depositAccountID, Debit: amount, Credit: 0},
		{AccountID: revenueAccountID, Debit: 0, Credit: amount},
	}

	entryID, err := model.CreateJournalEntry(h.DB, je, lines)
	if err != nil {
		fd := h.newIncomeFormData()
		fd.Entry = je
		fd.Amount = amount
		fd.RevenueAccount = revenueAccountID
		fd.DepositAccount = depositAccountID
		fd.Errors = map[string]string{"general": err.Error()}
		h.render(w, r, "templates/income/form.html", "Record Income", fd)
		return
	}

	h.setFlash(w, "Income recorded successfully")
	http.Redirect(w, r, fmt.Sprintf("/journals/%d", entryID), http.StatusSeeOther)
}

func (h *Handler) EditIncome(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	je, err := model.GetJournalEntry(h.DB, id)
	if err != nil || je.SourceType != "income" {
		http.NotFound(w, r)
		return
	}

	fd := h.newIncomeFormData()
	fd.Entry = je
	fd.IsEdit = true

	// Extract amount and accounts from lines
	for _, l := range je.Lines {
		if l.Debit > 0 {
			fd.DepositAccount = l.AccountID
			fd.Amount = l.Debit
		}
		if l.Credit > 0 {
			fd.RevenueAccount = l.AccountID
		}
	}

	h.render(w, r, "templates/income/form.html", "Edit Income", fd)
}

func (h *Handler) UpdateIncome(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user := auth.UserFromContext(r.Context())
	amount := parseIDR(r.FormValue("amount"))
	revenueAccountID, _ := strconv.Atoi(r.FormValue("revenue_account"))
	depositAccountID, _ := strconv.Atoi(r.FormValue("deposit_account"))

	je := &model.JournalEntry{
		ID:          id,
		EntryDate:   r.FormValue("entry_date"),
		Description: r.FormValue("description"),
		SourceType:  "income",
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
	if revenueAccountID == 0 {
		errors["revenue_account"] = "Revenue account is required"
	}
	if depositAccountID == 0 {
		errors["deposit_account"] = "Deposit account is required"
	}

	if len(errors) > 0 {
		fd := h.newIncomeFormData()
		fd.Entry = je
		fd.Amount = amount
		fd.RevenueAccount = revenueAccountID
		fd.DepositAccount = depositAccountID
		fd.Errors = errors
		fd.IsEdit = true
		h.render(w, r, "templates/income/form.html", "Edit Income", fd)
		return
	}

	lines := []model.JournalLine{
		{AccountID: depositAccountID, Debit: amount, Credit: 0},
		{AccountID: revenueAccountID, Debit: 0, Credit: amount},
	}

	if err := model.UpdateJournalEntry(h.DB, je, lines); err != nil {
		fd := h.newIncomeFormData()
		fd.Entry = je
		fd.Amount = amount
		fd.RevenueAccount = revenueAccountID
		fd.DepositAccount = depositAccountID
		fd.Errors = map[string]string{"general": err.Error()}
		fd.IsEdit = true
		h.render(w, r, "templates/income/form.html", "Edit Income", fd)
		return
	}

	h.setFlash(w, "Income updated successfully")
	http.Redirect(w, r, fmt.Sprintf("/journals/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteIncome(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Override source_type to allow deletion
	h.DB.Exec("UPDATE journal_entries SET source_type = 'manual' WHERE id = ? AND source_type = 'income'", id)
	model.DeleteJournalEntry(h.DB, id)

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "Income deleted successfully")
	http.Redirect(w, r, "/income", http.StatusSeeOther)
}

func (h *Handler) newIncomeFormData() incomeFormData {
	active := true
	revenueAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "revenue", IsActive: &active})
	depositAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "asset", IsActive: &active})

	return incomeFormData{
		Entry:           &model.JournalEntry{},
		RevenueAccounts: revenueAccounts,
		DepositAccounts: depositAccounts,
		Errors:          make(map[string]string),
	}
}
