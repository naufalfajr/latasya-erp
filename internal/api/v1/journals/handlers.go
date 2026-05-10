// Package journals implements the /api/v1/journals CRUD endpoints.
package journals

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

const maxLinesPerEntry = 100

type Handler struct {
	DB *sql.DB
}

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

// parseIDR parses an integer-IDR string (no decimals, no separators).
// Empty string is treated as 0.
func parseIDR(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, errors.New("must be non-negative")
	}
	return n, nil
}

func validateInput(inp *journalInput) (map[string]string, []model.JournalLine) {
	fields := map[string]string{}
	if strings.TrimSpace(inp.EntryDate) == "" {
		fields["entry_date"] = "required"
	}
	if strings.TrimSpace(inp.Description) == "" {
		fields["description"] = "required"
	}
	if len(inp.Lines) < 2 {
		fields["lines"] = "at least two lines required"
	}
	if len(inp.Lines) > maxLinesPerEntry {
		fields["lines"] = "too many lines (max 100)"
	}

	var totalDebit, totalCredit int
	lines := make([]model.JournalLine, 0, len(inp.Lines))
	for i, l := range inp.Lines {
		if l.AccountID <= 0 {
			fields["lines["+strconv.Itoa(i)+"].account_id"] = "required"
			continue
		}
		debit, err := parseIDR(l.Debit)
		if err != nil {
			fields["lines["+strconv.Itoa(i)+"].debit"] = "invalid amount"
			continue
		}
		credit, err := parseIDR(l.Credit)
		if err != nil {
			fields["lines["+strconv.Itoa(i)+"].credit"] = "invalid amount"
			continue
		}
		if debit > 0 && credit > 0 {
			fields["lines["+strconv.Itoa(i)+"]"] = "cannot have both debit and credit"
			continue
		}
		totalDebit += debit
		totalCredit += credit
		lines = append(lines, model.JournalLine{
			AccountID: l.AccountID,
			Debit:     debit,
			Credit:    credit,
			Memo:      l.Memo,
		})
	}

	if len(fields) == 0 {
		if totalDebit == 0 || totalCredit == 0 {
			fields["lines"] = "must have at least one debit and one credit line"
		} else if totalDebit != totalCredit {
			fields["lines"] = "debits must equal credits"
		}
	}

	if len(fields) > 0 {
		return fields, nil
	}
	return nil, lines
}

// List handles GET /api/v1/journals.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	filter := model.JournalFilter{
		DateFrom:   r.URL.Query().Get("from"),
		DateTo:     r.URL.Query().Get("to"),
		SourceType: r.URL.Query().Get("source"),
		Search:     r.URL.Query().Get("search"),
	}

	entries, err := model.ListJournalEntries(h.DB, filter)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list journals", nil)
		return
	}
	if entries == nil {
		entries = []model.JournalEntry{}
	}

	total := len(entries)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}

	v1.WriteList(w, http.StatusOK, entries[start:end], page, total)
}

// Get handles GET /api/v1/journals/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}

	je, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": je})
}

// Create handles POST /api/v1/journals.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapJournalsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "journals.manage capability required", nil)
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	var inp journalInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, lines := validateInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	ref, err := model.GenerateDocNumber(h.DB, "journal_entries", "reference", "JE")
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to generate reference", nil)
		return
	}

	je := &model.JournalEntry{
		EntryDate:   inp.EntryDate,
		Reference:   ref,
		Description: inp.Description,
		SourceType:  "manual",
		IsPosted:    true,
		CreatedBy:   user.ID,
	}

	id, err := model.CreateJournalEntry(h.DB, je, lines)
	if err != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, err.Error(), nil)
		return
	}

	created, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load created entry", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "journal.create",
		TargetType:  "journal_entry",
		TargetID:    int64(id),
		TargetLabel: created.Reference,
		Metadata: map[string]any{
			"after": map[string]any{
				"reference":   created.Reference,
				"entry_date":  created.EntryDate,
				"description": created.Description,
				"line_count":  len(created.Lines),
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": created})
}

// Update handles PUT /api/v1/journals/{id}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapJournalsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "journals.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}

	existing, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}

	if existing.SourceType != "" && existing.SourceType != "manual" {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict,
			"cannot edit auto-generated journal entry", nil)
		return
	}

	var inp journalInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, lines := validateInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	je := &model.JournalEntry{
		ID:          id,
		EntryDate:   inp.EntryDate,
		Description: inp.Description,
	}
	if err := model.UpdateJournalEntry(h.DB, je, lines); err != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, err.Error(), nil)
		return
	}

	updated, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load entry", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "journal.update",
		TargetType:  "journal_entry",
		TargetID:    int64(id),
		TargetLabel: updated.Reference,
		Metadata: map[string]any{
			"before": map[string]any{
				"entry_date":  existing.EntryDate,
				"description": existing.Description,
			},
			"after": map[string]any{
				"entry_date":  updated.EntryDate,
				"description": updated.Description,
			},
		},
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}

// Delete handles DELETE /api/v1/journals/{id}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapJournalsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "journals.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}

	existing, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "journal entry not found", nil)
		return
	}

	if err := model.DeleteJournalEntry(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "journal.delete",
		TargetType:  "journal_entry",
		TargetID:    int64(id),
		TargetLabel: existing.Reference,
		Metadata: map[string]any{
			"before": map[string]any{
				"reference":   existing.Reference,
				"entry_date":  existing.EntryDate,
				"description": existing.Description,
			},
		},
	})

	w.WriteHeader(http.StatusNoContent)
}
