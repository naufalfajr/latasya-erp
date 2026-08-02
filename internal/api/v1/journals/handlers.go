// Package journals implements the /api/v1/journals CRUD endpoints.
package journals

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Journals *journal.Module }

type lineInput struct {
	AccountID int    `json:"account_id"`
	Debit     string `json:"debit"`
	Credit    string `json:"credit"`
	Memo      string `json:"memo"`
}

type journalInput struct {
	EntryDate   string      `json:"entry_date"`
	Description string      `json:"description"`
	Lines       []lineInput `json:"lines"`
}

func actor(r *http.Request) journal.Actor {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		return journal.Actor{}
	}
	return journal.Actor{UserID: user.ID, CanManageJournals: v1.HasEffectiveCapability(r.Context(), model.CapJournalsManage)}
}

func draft(input journalInput) (journal.ManualDraft, map[string]string) {
	fields := map[string]string{}
	lines := make([]journal.Line, 0, len(input.Lines))
	for i, inputLine := range input.Lines {
		debit, err := parseIDR(inputLine.Debit)
		if err != nil {
			fields["lines["+strconv.Itoa(i)+"].debit"] = "invalid amount"
		}
		credit, err := parseIDR(inputLine.Credit)
		if err != nil {
			fields["lines["+strconv.Itoa(i)+"].credit"] = "invalid amount"
		}
		lines = append(lines, journal.Line{AccountID: inputLine.AccountID, Debit: debit, Credit: credit, Memo: inputLine.Memo})
	}
	if len(fields) > 0 {
		return journal.ManualDraft{}, fields
	}
	return journal.ManualDraft{EntryDate: input.EntryDate, Description: input.Description, Lines: lines}, nil
}

func parseIDR(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	amount, err := strconv.Atoi(value)
	if err != nil || amount < 0 {
		return 0, errors.New("invalid amount")
	}
	return amount, nil
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	result, err := h.Journals.List(r.Context(), journal.Filter{DateFrom: r.URL.Query().Get("from"),
		DateTo: r.URL.Query().Get("to"), SourceType: r.URL.Query().Get("source"), Search: r.URL.Query().Get("search"),
		Limit: page.PerPage, Offset: page.Offset()})
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list journals", nil)
		return
	}
	v1.WriteList(w, http.StatusOK, result.Entries, page, result.Total)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}
	entry, err := h.Journals.Get(r.Context(), id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": entry})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !requireManage(w, r) {
		return
	}
	var input journalInput
	if err := v1.DecodeJSON(w, r, &input); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	command, fields := draft(input)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	created, err := h.Journals.CreateManual(r.Context(), actor(r), command)
	if !writeModuleError(w, r, err) {
		return
	}
	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": created})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !requireManage(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}
	var input journalInput
	if err := v1.DecodeJSON(w, r, &input); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	command, fields := draft(input)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	updated, err := h.Journals.UpdateManual(r.Context(), actor(r), id, command)
	if !writeModuleError(w, r, err) {
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !requireManage(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}
	_, err = h.Journals.DeleteManual(r.Context(), actor(r), id)
	if !writeModuleError(w, r, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requireManage(w http.ResponseWriter, r *http.Request) bool {
	if v1.HasEffectiveCapability(r.Context(), model.CapJournalsManage) {
		return true
	}
	v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "journals.manage capability required", nil)
	return false
}

func writeModuleError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return true
	}
	var validation *journal.ValidationError
	var conflict *journal.ConflictError
	switch {
	case errors.Is(err, journal.ErrForbidden):
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "journals.manage capability required", nil)
	case errors.Is(err, journal.ErrNotFound):
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
	case errors.As(err, &validation):
		fields := validation.Fields
		if balance, ok := fields["balance"]; ok {
			fields = make(map[string]string, len(validation.Fields))
			for key, value := range validation.Fields {
				fields[key] = value
			}
			delete(fields, "balance")
			fields["lines"] = balance
		}
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, validation.Error(), fields)
	case errors.As(err, &conflict):
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, conflict.Error(), nil)
	default:
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "journal operation failed", nil)
	}
	return false
}
