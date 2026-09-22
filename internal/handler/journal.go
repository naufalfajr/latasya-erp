package handler

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
)

type journalPageData struct {
	Entries       []model.JournalEntry
	Filter        journal.Filter
	AccountGroups []journalAccountGroup
	AccountValue  string
	AccountError  string
	HasFilters    bool
	ReturnTo      string
	Pagination    Pagination
}

type journalAccountGroup struct {
	Label    string
	Accounts []model.Account
}

type journalFormData struct {
	Entry    *model.JournalEntry
	Lines    []model.JournalLine
	Accounts []model.Account
	Errors   map[string]string
	IsEdit   bool
	ReturnTo string
}

const journalFormTemplate = "templates/journals/form.html"
const journalLinePartial = "templates/journals/line_partial.html"

func (h *Handler) ListJournals(w http.ResponseWriter, r *http.Request) {
	filter := journal.Filter{DateFrom: r.URL.Query().Get("from"), DateTo: r.URL.Query().Get("to"),
		SourceType: r.URL.Query().Get("source"), Search: r.URL.Query().Get("search")}
	accountValue := r.URL.Query().Get("account")
	accountOptions, err := h.Journals.AccountFilterOptions(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	accountID, accountError := resolveJournalAccount(accountValue, r.URL.Query()["account"], accountOptions)
	filter.AccountID = accountID
	page := parsePage(r)
	filter.Limit, filter.Offset = listPageSize, (page-1)*listPageSize
	result := &journal.ListResult{Entries: make([]model.JournalEntry, 0)}
	if accountError == "" {
		result, err = h.Journals.List(r.Context(), filter)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		entryIDs := make([]int, len(result.Entries))
		for i, entry := range result.Entries {
			entryIDs[i] = entry.ID
		}
		summaries, err := h.Journals.AccountSummaries(r.Context(), entryIDs)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		for i := range result.Entries {
			if summary, ok := summaries[result.Entries[i].ID]; ok {
				result.Entries[i].ListAccountCode = summary.AccountCode
				result.Entries[i].ListAccountName = summary.AccountName
				result.Entries[i].JournalLineCount = summary.LineCount
			}
		}
	}
	pg := newPagination(page, result.Total)
	data := journalPageData{
		Entries: result.Entries, Filter: filter, AccountGroups: groupJournalAccounts(accountOptions),
		AccountValue: accountValue, AccountError: accountError,
		HasFilters: accountValue != "" || filter.DateFrom != "" || filter.DateTo != "" || filter.Search != "",
		ReturnTo:   currentListURL(r, h.BasePath),
		Pagination: pg,
	}
	if isHTMXTarget(r, "journal-table") {
		h.renderFragment(w, r, "templates/journals/index.html", "journal-table", data)
		return
	}
	h.render(w, r, "templates/journals/index.html", "Journal Entries", data)
}

func resolveJournalAccount(value string, values []string, options []model.Account) (int, string) {
	if value == "" && len(values) == 0 {
		return 0, ""
	}
	if len(values) > 1 {
		return 0, "The selected account is invalid or unavailable. Reset filters to view all entries."
	}
	if value == "" {
		return 0, ""
	}
	id, err := strconv.Atoi(value)
	if err != nil || id < 1 {
		return 0, "The selected account is invalid or unavailable. Reset filters to view all entries."
	}
	for _, option := range options {
		if option.ID == id {
			return id, ""
		}
	}
	return 0, "The selected account is invalid or unavailable. Reset filters to view all entries."
}

func groupJournalAccounts(accounts []model.Account) []journalAccountGroup {
	groups := make([]journalAccountGroup, 0, 5)
	groupIndex := make(map[string]int, 5)
	for _, account := range accounts {
		index, ok := groupIndex[account.AccountType]
		if !ok {
			index = len(groups)
			groupIndex[account.AccountType] = index
			groups = append(groups, journalAccountGroup{Label: model.AccountTypeLabel(account.AccountType)})
		}
		groups[index].Accounts = append(groups[index].Accounts, account)
	}
	return groups
}

func currentListURL(r *http.Request, basePath string) string {
	path := r.URL.Path
	basePath = strings.TrimSuffix(basePath, "/")
	if basePath != "" && strings.HasPrefix(path, basePath+"/") {
		path = strings.TrimPrefix(path, basePath)
	}
	value := path
	if r.URL.RawQuery != "" {
		value += "?" + r.URL.RawQuery
	}
	return value
}

func (h *Handler) renderJournalForm(w http.ResponseWriter, r *http.Request, title string, data journalFormData) {
	if data.Errors == nil {
		data.Errors = map[string]string{}
	}
	h.render(w, r, journalFormTemplate, title, data, journalLinePartial)
}

func (h *Handler) NewJournal(w http.ResponseWriter, r *http.Request) {
	options, err := h.Journals.Options(r.Context(), "", false)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.renderJournalForm(w, r, "New Journal Entry", journalFormData{Entry: &model.JournalEntry{IsPosted: true},
		Lines: []model.JournalLine{{}, {}}, Accounts: options.Accounts})
}

func (h *Handler) CreateJournal(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	entry := &model.JournalEntry{EntryDate: r.FormValue("entry_date"), Description: r.FormValue("description"), SourceType: model.SourceManual, IsPosted: true}
	lines := parseJournalLines(r)
	created, err := h.Journals.CreateManual(r.Context(), journalActor(r), journal.ManualDraft{
		EntryDate: entry.EntryDate, Description: entry.Description, Lines: toModuleLines(lines),
	})
	if err != nil {
		h.renderJournalError(w, r, "New Journal Entry", entry, lines, false, err)
		return
	}
	h.setFlash(w, "Journal entry created successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/journals/%d", created.ID), http.StatusSeeOther)
}

func (h *Handler) ViewJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	entry, err := h.Journals.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, "templates/journals/view.html", "Journal Entry "+entry.Reference, h.journalViewData(r, entry))
}

func (h *Handler) EditJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	entry, err := h.Journals.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if entry.SourceType != "" && entry.SourceType != model.SourceManual {
		h.setFlash(w, "Cannot edit auto-generated journal entries")
		http.Redirect(w, r, h.journalDetailURL(id, journalReturnToFromRequest(r, entry.SourceType, h.BasePath)), http.StatusSeeOther)
		return
	}
	options, err := h.Journals.Options(r.Context(), "", false)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.renderJournalForm(w, r, "Edit Journal Entry", journalFormData{Entry: entry, Lines: entry.Lines, Accounts: options.Accounts,
		IsEdit: true, ReturnTo: journalEditReturnToFromRequest(r, entry.SourceType, h.BasePath)})
}

func (h *Handler) UpdateJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.Journals.Get(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	entry := &model.JournalEntry{ID: id, EntryDate: r.FormValue("entry_date"), Description: r.FormValue("description")}
	lines := parseJournalLines(r)
	_, err = h.Journals.UpdateManual(r.Context(), journalActor(r), id, journal.ManualDraft{
		EntryDate: entry.EntryDate, Description: entry.Description, Lines: toModuleLines(lines),
	})
	if err != nil {
		if errors.Is(err, journal.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.renderJournalError(w, r, "Edit Journal Entry", entry, lines, true, err)
		return
	}
	h.setFlash(w, "Journal entry updated successfully")
	http.Redirect(w, r, h.journalUpdatedDetailURL(id, r, model.SourceManual), http.StatusSeeOther)
}

func (h *Handler) DeleteJournal(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	returnTo := journalReturnToFromRequest(r, model.SourceManual, h.BasePath)
	if _, err := h.Journals.DeleteManual(r.Context(), journalActor(r), id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		h.finishJournalDelete(w, r, h.journalDetailURL(id, returnTo))
		return
	}
	h.setFlash(w, "Journal entry deleted successfully")
	h.finishJournalDelete(w, r, h.BasePath+returnTo)
}

func (h *Handler) JournalLinePartial(w http.ResponseWriter, r *http.Request) {
	options, err := h.Journals.Options(r.Context(), "", false)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	t, err := template.New("").Funcs(h.FuncMap).ParseFS(h.TemplateFS, journalLinePartial)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	_ = t.ExecuteTemplate(w, "journal-line", struct{ Accounts []model.Account }{Accounts: options.Accounts})
}

func (h *Handler) renderJournalError(w http.ResponseWriter, r *http.Request, title string, entry *model.JournalEntry, lines []model.JournalLine, edit bool, err error) {
	options, optionErr := h.Journals.Options(r.Context(), "", false)
	if optionErr != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	errorsByField := moduleFields(err)
	if len(errorsByField) == 0 {
		errorsByField["general"] = err.Error()
	}
	h.renderJournalForm(w, r, title, journalFormData{Entry: entry, Lines: lines, Accounts: options.Accounts,
		Errors: errorsByField, IsEdit: edit, ReturnTo: journalEditReturnToFromForm(r, model.SourceManual, h.BasePath)})
}

func journalActor(r *http.Request) journal.Actor {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		return journal.Actor{}
	}
	return journal.Actor{UserID: user.ID, CanManageJournals: user.HasCapability(model.CapJournalsManage)}
}

func toModuleLines(lines []model.JournalLine) []journal.Line {
	result := make([]journal.Line, 0, len(lines))
	for _, line := range lines {
		result = append(result, journal.Line{AccountID: line.AccountID, Debit: line.Debit, Credit: line.Credit, Memo: line.Memo})
	}
	return result
}

func moduleFields(err error) map[string]string {
	var validation *journal.ValidationError
	if errors.As(err, &validation) {
		fields := make(map[string]string, len(validation.Fields))
		for key, value := range validation.Fields {
			fields[key] = value
		}
		if value, ok := fields["balance"]; ok {
			fields["balance"] = value
		}
		return fields
	}
	return map[string]string{}
}

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
		if accountID == 0 && debit == 0 && credit == 0 {
			continue
		}
		lines = append(lines, model.JournalLine{AccountID: accountID, Debit: debit, Credit: credit, Memo: getIndex(memos, i)})
	}
	return lines
}
