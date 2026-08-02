package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
)

type incomeFormData struct {
	Entry           *model.JournalEntry
	Amount          int
	RevenueAccount  int
	DepositAccount  int
	RevenueAccounts []model.Account
	DepositAccounts []model.Account
	Errors          map[string]string
	IsEdit          bool
}

func (h *Handler) ListIncome(w http.ResponseWriter, r *http.Request) {
	filter := journal.Filter{SourceType: model.SourceIncome, DateFrom: r.URL.Query().Get("from"),
		DateTo: r.URL.Query().Get("to"), Search: r.URL.Query().Get("search")}
	page := parsePage(r)
	filter.Limit, filter.Offset = listPageSize, (page-1)*listPageSize
	result, err := h.Journals.List(r.Context(), filter)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	pg := newPagination(page, result.Total)
	h.render(w, r, "templates/income/index.html", "Income", map[string]any{
		"Entries": result.Entries, "Pagination": newPageNav(pg, map[string]string{"from": filter.DateFrom, "to": filter.DateTo, "search": filter.Search}),
	})
}

func (h *Handler) NewIncome(w http.ResponseWriter, r *http.Request) {
	form, err := h.newIncomeFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/income/form.html", "Record Income", form)
}

func (h *Handler) CreateIncome(w http.ResponseWriter, r *http.Request) {
	draft := incomeDraftFromForm(r)
	created, err := h.Journals.CreateIncome(r.Context(), incomeActor(r), draft)
	if err != nil {
		h.renderIncomeError(w, r, "Record Income", draft, false, err)
		return
	}
	h.setFlash(w, "Income recorded successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/journals/%d", created.ID), http.StatusSeeOther)
}

func (h *Handler) EditIncome(w http.ResponseWriter, r *http.Request) {
	entry, ok := h.incomeEntry(w, r)
	if !ok {
		return
	}
	form, err := h.newIncomeFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	form.Entry, form.IsEdit = entry, true
	form.Amount, form.RevenueAccount, form.DepositAccount = extractIncomeShape(entry)
	h.render(w, r, "templates/income/form.html", "Edit Income", form)
}

func (h *Handler) UpdateIncome(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if entry, err := h.Journals.Get(r.Context(), id); err != nil || entry.SourceType != model.SourceIncome {
		http.NotFound(w, r)
		return
	}
	draft := incomeDraftFromForm(r)
	if _, err := h.Journals.UpdateIncome(r.Context(), incomeActor(r), id, draft); err != nil {
		h.renderIncomeError(w, r, "Edit Income", draft, true, err)
		return
	}
	h.setFlash(w, "Income updated successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/journals/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteIncome(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.Journals.DeleteIncome(r.Context(), incomeActor(r), id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, h.BasePath+"/income", http.StatusSeeOther)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.setFlash(w, "Income deleted successfully")
	http.Redirect(w, r, h.BasePath+"/income", http.StatusSeeOther)
}

func incomeDraftFromForm(r *http.Request) journal.IncomeDraft {
	revenue, _ := strconv.Atoi(r.FormValue("revenue_account"))
	deposit, _ := strconv.Atoi(r.FormValue("deposit_account"))
	return journal.IncomeDraft{EntryDate: r.FormValue("entry_date"), Description: r.FormValue("description"), Amount: parseIDR(r.FormValue("amount")),
		RevenueAccount: revenue, DepositAccount: deposit}
}

func (h *Handler) newIncomeFormData(r *http.Request) (incomeFormData, error) {
	revenue, err := h.Journals.Options(r.Context(), model.AccountTypeRevenue, false)
	if err != nil {
		return incomeFormData{}, err
	}
	deposit, err := h.Journals.Options(r.Context(), model.AccountTypeAsset, false)
	if err != nil {
		return incomeFormData{}, err
	}
	return incomeFormData{Entry: &model.JournalEntry{}, RevenueAccounts: revenue.Accounts, DepositAccounts: deposit.Accounts, Errors: map[string]string{}}, nil
}

func (h *Handler) renderIncomeError(w http.ResponseWriter, r *http.Request, title string, draft journal.IncomeDraft, edit bool, err error) {
	form, optionErr := h.newIncomeFormData(r)
	if optionErr != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	form.Entry = &model.JournalEntry{ID: parsePathID(r), EntryDate: draft.EntryDate, Description: draft.Description, SourceType: model.SourceIncome, IsPosted: true}
	form.Amount, form.RevenueAccount, form.DepositAccount = draft.Amount, draft.RevenueAccount, draft.DepositAccount
	form.Errors, form.IsEdit = moduleFields(err), edit
	if len(form.Errors) == 0 {
		form.Errors["general"] = err.Error()
	}
	h.render(w, r, "templates/income/form.html", title, form)
}

func (h *Handler) incomeEntry(w http.ResponseWriter, r *http.Request) (*model.JournalEntry, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	entry, err := h.Journals.Get(r.Context(), id)
	if err != nil || entry.SourceType != model.SourceIncome {
		http.NotFound(w, r)
		return nil, false
	}
	return entry, true
}

func incomeActor(r *http.Request) journal.Actor {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		return journal.Actor{}
	}
	return journal.Actor{UserID: user.ID, CanManageIncome: user.HasCapability(model.CapIncomeManage)}
}

func extractIncomeShape(entry *model.JournalEntry) (amount, revenueAccount, depositAccount int) {
	for _, line := range entry.Lines {
		if line.Debit > 0 {
			depositAccount, amount = line.AccountID, line.Debit
		}
		if line.Credit > 0 {
			revenueAccount = line.AccountID
		}
	}
	return
}

func parsePathID(r *http.Request) int {
	id, _ := strconv.Atoi(r.PathValue("id"))
	return id
}
