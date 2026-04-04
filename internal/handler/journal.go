package handler

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type journalPageData struct {
	Entries []model.JournalEntry
	Filter  model.JournalFilter
}

func (h *Handler) ListJournals(w http.ResponseWriter, r *http.Request) {
	f := model.JournalFilter{
		DateFrom:   r.URL.Query().Get("from"),
		DateTo:     r.URL.Query().Get("to"),
		SourceType: r.URL.Query().Get("source"),
		Search:     r.URL.Query().Get("search"),
	}

	entries, err := model.ListJournalEntries(h.DB, f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/journals/index.html", "Journal Entries", journalPageData{
		Entries: entries,
		Filter:  f,
	})
}

type journalFormData struct {
	Entry    *model.JournalEntry
	Lines    []model.JournalLine
	Accounts []model.Account
	Errors   map[string]string
	IsEdit   bool
}

const journalFormTemplate = "templates/journals/form.html"
const journalLinePartial = "templates/journals/line_partial.html"

func (h *Handler) renderJournalForm(w http.ResponseWriter, r *http.Request, title string, data journalFormData) {
	if data.Errors == nil {
		data.Errors = make(map[string]string)
	}
	h.render(w, r, journalFormTemplate, title, data, journalLinePartial)
}

func (h *Handler) NewJournal(w http.ResponseWriter, r *http.Request) {
	active := true
	accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})

	h.renderJournalForm(w, r, "New Journal Entry", journalFormData{
		Entry: &model.JournalEntry{IsPosted: true},
		Lines: []model.JournalLine{
			{}, // Start with 2 empty lines
			{},
		},
		Accounts: accounts,
	})
}

func (h *Handler) CreateJournal(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	user := auth.UserFromContext(r.Context())

	je := &model.JournalEntry{
		EntryDate:   r.FormValue("entry_date"),
		Description: r.FormValue("description"),
		SourceType:  "manual",
		IsPosted:    true,
		CreatedBy:   user.ID,
	}

	lines := parseJournalLines(r)
	errors := validateJournal(je, lines)

	if len(errors) > 0 {
		active := true
		accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})
		h.renderJournalForm(w, r, "New Journal Entry", journalFormData{
			Entry:    je,
			Lines:    lines,
			Accounts: accounts,
			Errors:   errors,
		})
		return
	}

	entryID, err := model.CreateJournalEntry(h.DB, je, lines)
	if err != nil {
		active := true
		accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})
		errors["general"] = err.Error()
		h.renderJournalForm(w, r, "New Journal Entry", journalFormData{
			Entry:    je,
			Lines:    lines,
			Accounts: accounts,
			Errors:   errors,
		})
		return
	}

	h.setFlash(w, "Journal entry created successfully")
	http.Redirect(w, r, fmt.Sprintf("/journals/%d", entryID), http.StatusSeeOther)
}

func (h *Handler) ViewJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	je, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	h.render(w, r, "templates/journals/view.html", "Journal Entry "+je.Reference, je)
}

func (h *Handler) EditJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	je, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Don't allow editing auto-generated entries
	if je.SourceType != "" && je.SourceType != "manual" {
		h.setFlash(w, "Cannot edit auto-generated journal entries")
		http.Redirect(w, r, fmt.Sprintf("/journals/%d", id), http.StatusSeeOther)
		return
	}

	active := true
	accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})

	h.renderJournalForm(w, r, "Edit Journal Entry", journalFormData{
		Entry:    je,
		Lines:    je.Lines,
		Accounts: accounts,
		IsEdit:   true,
	})
}

func (h *Handler) UpdateJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	r.ParseForm()

	je := &model.JournalEntry{
		ID:          id,
		EntryDate:   r.FormValue("entry_date"),
		Description: r.FormValue("description"),
	}

	lines := parseJournalLines(r)
	errors := validateJournal(je, lines)

	if len(errors) > 0 {
		active := true
		accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})
		h.renderJournalForm(w, r, "Edit Journal Entry", journalFormData{
			Entry:    je,
			Lines:    lines,
			Accounts: accounts,
			Errors:   errors,
			IsEdit:   true,
		})
		return
	}

	if err := model.UpdateJournalEntry(h.DB, je, lines); err != nil {
		active := true
		accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})
		errors["general"] = err.Error()
		h.renderJournalForm(w, r, "Edit Journal Entry", journalFormData{
			Entry:    je,
			Lines:    lines,
			Accounts: accounts,
			Errors:   errors,
			IsEdit:   true,
		})
		return
	}

	h.setFlash(w, "Journal entry updated successfully")
	http.Redirect(w, r, fmt.Sprintf("/journals/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := model.DeleteJournalEntry(h.DB, id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, fmt.Sprintf("/journals/%d", id), http.StatusSeeOther)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "Journal entry deleted successfully")
	http.Redirect(w, r, "/journals", http.StatusSeeOther)
}

// JournalLinePartial returns an empty journal line row for HTMX "Add Line"
func (h *Handler) JournalLinePartial(w http.ResponseWriter, r *http.Request) {
	active := true
	accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})

	t, err := template.New("").Funcs(h.FuncMap).ParseFS(h.TemplateFS, "templates/journals/line_partial.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Accounts []model.Account
	}{
		Accounts: accounts,
	}

	t.ExecuteTemplate(w, "journal-line", data)
}

// parseJournalLines extracts journal lines from form data
func parseJournalLines(r *http.Request) []model.JournalLine {
	accountIDs := r.Form["line_account_id"]
	debits := r.Form["line_debit"]
	credits := r.Form["line_credit"]
	memos := r.Form["line_memo"]

	var lines []model.JournalLine
	for i := range accountIDs {
		accountID, _ := strconv.Atoi(accountIDs[i])
		debit := parseIDR(getIndex(debits, i))
		credit := parseIDR(getIndex(credits, i))

		// Skip completely empty lines
		if accountID == 0 && debit == 0 && credit == 0 {
			continue
		}

		lines = append(lines, model.JournalLine{
			AccountID: accountID,
			Debit:     debit,
			Credit:    credit,
			Memo:      getIndex(memos, i),
		})
	}
	return lines
}

func validateJournal(je *model.JournalEntry, lines []model.JournalLine) map[string]string {
	errors := make(map[string]string)

	if je.EntryDate == "" {
		errors["entry_date"] = "Date is required"
	}
	if je.Description == "" {
		errors["description"] = "Description is required"
	}
	if len(lines) < 2 {
		errors["lines"] = "At least 2 lines are required"
	}

	var totalDebit, totalCredit int
	for i, l := range lines {
		if l.AccountID == 0 {
			errors[fmt.Sprintf("line_%d_account", i)] = "Account is required"
		}
		if l.Debit == 0 && l.Credit == 0 {
			errors[fmt.Sprintf("line_%d_amount", i)] = "Debit or credit amount is required"
		}
		if l.Debit > 0 && l.Credit > 0 {
			errors[fmt.Sprintf("line_%d_amount", i)] = "Line cannot have both debit and credit"
		}
		totalDebit += l.Debit
		totalCredit += l.Credit
	}

	if len(lines) >= 2 && totalDebit != totalCredit {
		errors["balance"] = fmt.Sprintf("Debits (%d) must equal credits (%d)", totalDebit, totalCredit)
	}

	return errors
}

