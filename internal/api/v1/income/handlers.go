// Package income implements the /api/v1/income CRUD endpoints.
package income

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/audit"
	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// Handler handles /api/v1/income endpoints.
type Handler struct {
	DB *sql.DB
}

// accountRef is a compact account reference returned in responses.
type accountRef struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// incomeEntry is the canonical JSON representation of an income record.
type incomeEntry struct {
	ID             int         `json:"id"`
	Reference      string      `json:"reference"`
	EntryDate      string      `json:"entry_date"`
	Description    string      `json:"description"`
	Amount         string      `json:"amount"` // IDR as integer string
	RevenueAccount *accountRef `json:"revenue_account,omitempty"`
	DepositAccount *accountRef `json:"deposit_account,omitempty"`
	CreatedAt      string      `json:"created_at"`
}

// incomeInput is the JSON request body for Create and Update.
type incomeInput struct {
	EntryDate      string `json:"entry_date"`
	Description    string `json:"description"`
	Amount         string `json:"amount"` // IDR as integer string
	RevenueAccount int    `json:"revenue_account"`
	DepositAccount int    `json:"deposit_account"`
}

// toIncomeEntry converts a JournalEntry to the API response shape.
// For list entries (no Lines loaded), account refs will be nil.
func toIncomeEntry(je *model.JournalEntry) incomeEntry {
	e := incomeEntry{
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
			// Deposit account: asset that receives the payment (debit increases asset)
			e.DepositAccount = &accountRef{ID: l.AccountID, Code: l.AccountCode, Name: l.AccountName}
		}
		if l.Credit > 0 {
			// Revenue account: credited when income is recorded
			e.RevenueAccount = &accountRef{ID: l.AccountID, Code: l.AccountCode, Name: l.AccountName}
		}
	}
	return e
}

func validateIncomeInput(inp *incomeInput) map[string]string {
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
	if inp.RevenueAccount == 0 {
		fields["revenue_account"] = "required"
	}
	if inp.DepositAccount == 0 {
		fields["deposit_account"] = "required"
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// extractIncomeShape extracts amount + revenue + deposit account IDs from journal lines.
func extractIncomeShape(je *model.JournalEntry) (amount, revenueAccount, depositAccount int) {
	for _, l := range je.Lines {
		if l.Debit > 0 {
			depositAccount = l.AccountID
			amount = l.Debit
		}
		if l.Credit > 0 {
			revenueAccount = l.AccountID
		}
	}
	return
}

// List handles GET /api/v1/income
// Query params: ?from=, ?to=, ?search=, ?page=, ?per_page=
// Auth: any authenticated user.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f := model.JournalFilter{
		SourceType: model.SourceIncome,
		DateFrom:   r.URL.Query().Get("from"),
		DateTo:     r.URL.Query().Get("to"),
		Search:     r.URL.Query().Get("search"),
	}

	entries, err := model.ListJournalEntries(h.DB, f)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list income entries", nil)
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

	result := make([]incomeEntry, 0, end-start)
	for _, je := range entries[start:end] {
		result = append(result, toIncomeEntry(&je))
	}

	v1.WriteList(w, http.StatusOK, result, page, total)
}

// Get handles GET /api/v1/income/{id}
// Auth: any authenticated user.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
		return
	}

	je, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
		return
	}

	if je.SourceType != model.SourceIncome {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
		return
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": toIncomeEntry(je)})
}

// Create handles POST /api/v1/income
// Requires: CapIncomeManage. Idempotency-Key supported.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapIncomeManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "income.manage capability required", nil)
		return
	}

	var inp incomeInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	if fields := validateIncomeInput(&inp); fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	amount, _ := strconv.Atoi(inp.Amount)

	user := auth.UserFromContext(r.Context())
	je := &model.JournalEntry{
		EntryDate:   inp.EntryDate,
		Description: inp.Description,
		SourceType:  model.SourceIncome,
		IsPosted:    true,
		CreatedBy:   user.ID,
	}

	lines := []model.JournalLine{
		{AccountID: inp.DepositAccount, Debit: amount, Credit: 0},
		{AccountID: inp.RevenueAccount, Debit: 0, Credit: amount},
	}

	entryID, err := model.CreateJournalEntry(h.DB, je, lines)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create income entry", nil)
		return
	}

	created, err := model.GetJournalEntry(h.DB, entryID)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve created income entry", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "income.create",
		TargetType:  "income",
		TargetID:    int64(entryID),
		TargetLabel: inp.Description,
		Metadata: map[string]any{
			"after": map[string]any{
				"entry_date":      inp.EntryDate,
				"description":     inp.Description,
				"amount":          amount,
				"revenue_account": inp.RevenueAccount,
				"deposit_account": inp.DepositAccount,
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": toIncomeEntry(created)})
}

// Update handles PUT /api/v1/income/{id}
// Requires: CapIncomeManage. Idempotency-Key supported.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapIncomeManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "income.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
		return
	}

	existing, err := model.GetJournalEntry(h.DB, id)
	if err != nil || existing.SourceType != model.SourceIncome {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
		return
	}

	var inp incomeInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	if fields := validateIncomeInput(&inp); fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	amount, _ := strconv.Atoi(inp.Amount)

	je := &model.JournalEntry{
		ID:          id,
		EntryDate:   inp.EntryDate,
		Description: inp.Description,
		SourceType:  model.SourceIncome,
		IsPosted:    true,
	}

	lines := []model.JournalLine{
		{AccountID: inp.DepositAccount, Debit: amount, Credit: 0},
		{AccountID: inp.RevenueAccount, Debit: 0, Credit: amount},
	}

	if err := model.UpdateJournalEntry(h.DB, je, lines); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update income entry", nil)
		return
	}

	updated, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve updated income entry", nil)
		return
	}

	oldAmount, oldRevenueAcct, oldDepositAcct := extractIncomeShape(existing)
	oldFields := map[string]any{
		"entry_date":      existing.EntryDate,
		"description":     existing.Description,
		"amount":          oldAmount,
		"revenue_account": oldRevenueAcct,
		"deposit_account": oldDepositAcct,
	}
	newFields := map[string]any{
		"entry_date":      inp.EntryDate,
		"description":     inp.Description,
		"amount":          amount,
		"revenue_account": inp.RevenueAccount,
		"deposit_account": inp.DepositAccount,
	}
	if metadata := audit.Diff(oldFields, newFields,
		[]string{"entry_date", "description", "amount", "revenue_account", "deposit_account"}); metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "income.update",
			TargetType:  "income",
			TargetID:    int64(id),
			TargetLabel: inp.Description,
			Metadata:    metadata,
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": toIncomeEntry(updated)})
}

// Delete handles DELETE /api/v1/income/{id}
// Requires: CapIncomeManage. Returns 204.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapIncomeManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "income.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
		return
	}

	existing, err := model.GetJournalEntry(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
		return
	}
	if existing.SourceType != model.SourceIncome {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "income entry not found", nil)
		return
	}

	if err := model.DeleteJournalEntryBySource(h.DB, id, model.SourceIncome); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	amount, revenueAcct, depositAcct := extractIncomeShape(existing)
	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "income.delete",
		TargetType:  "income",
		TargetID:    int64(id),
		TargetLabel: existing.Description,
		Metadata: map[string]any{
			"before": map[string]any{
				"entry_date":      existing.EntryDate,
				"description":     existing.Description,
				"amount":          amount,
				"revenue_account": revenueAcct,
				"deposit_account": depositAcct,
			},
		},
	})

	w.WriteHeader(http.StatusNoContent)
}
