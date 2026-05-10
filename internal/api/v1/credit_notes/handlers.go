// Package creditnotes implements the /api/v1/credit-notes endpoints with
// full credit-note lifecycle support (draft → issued → voided).
package creditnotes

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

// Handler holds shared dependencies for the credit notes API.
type Handler struct {
	DB *sql.DB
}

type lineInput struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	AccountID   int    `json:"account_id"`
}

type creditNoteInput struct {
	ContactID int         `json:"contact_id"`
	InvoiceID *int        `json:"invoice_id,omitempty"`
	CNDate    string      `json:"cn_date"`
	Reason    string      `json:"reason"`
	TaxAmount string      `json:"tax_amount"`
	Notes     string      `json:"notes"`
	Lines     []lineInput `json:"lines"`
}

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

func parseQuantity(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	parts := strings.SplitN(s, ".", 2)
	whole, err := strconv.Atoi(parts[0])
	if err != nil || whole < 0 {
		return 0, errors.New("invalid quantity")
	}
	frac := 0
	if len(parts) == 2 {
		fp := parts[1]
		if len(fp) == 0 || len(fp) > 2 {
			return 0, errors.New("invalid quantity")
		}
		if len(fp) == 1 {
			fp += "0"
		}
		frac, err = strconv.Atoi(fp)
		if err != nil || frac < 0 {
			return 0, errors.New("invalid quantity")
		}
	}
	return whole*100 + frac, nil
}

var validReasons = map[string]bool{
	model.CreditNoteReasonCancellation: true,
	model.CreditNoteReasonReturn:       true,
	model.CreditNoteReasonDiscount:     true,
	model.CreditNoteReasonOther:        true,
}

func validateInput(inp *creditNoteInput) (map[string]string, []model.CreditNoteLine, int) {
	fields := map[string]string{}
	if inp.ContactID == 0 {
		fields["contact_id"] = "required"
	}
	if strings.TrimSpace(inp.CNDate) == "" {
		fields["cn_date"] = "required"
	}
	if strings.TrimSpace(inp.Reason) == "" {
		fields["reason"] = "required"
	} else if !validReasons[inp.Reason] {
		fields["reason"] = "must be one of: cancellation, return, discount, other"
	}
	taxAmount, err := parseIDR(inp.TaxAmount)
	if err != nil {
		fields["tax_amount"] = "invalid amount"
	}
	if len(inp.Lines) == 0 {
		fields["lines"] = "at least one line required"
	}

	lines := make([]model.CreditNoteLine, 0, len(inp.Lines))
	for i, l := range inp.Lines {
		qty, qerr := parseQuantity(l.Quantity)
		if qerr != nil {
			fields["lines["+strconv.Itoa(i)+"].quantity"] = "invalid quantity"
			continue
		}
		if qty == 0 {
			qty = 100
		}
		price, perr := parseIDR(l.UnitPrice)
		if perr != nil {
			fields["lines["+strconv.Itoa(i)+"].unit_price"] = "invalid amount"
			continue
		}
		if strings.TrimSpace(l.Description) == "" {
			fields["lines["+strconv.Itoa(i)+"].description"] = "required"
			continue
		}
		if price <= 0 {
			fields["lines["+strconv.Itoa(i)+"].unit_price"] = "must be positive"
			continue
		}
		if l.AccountID == 0 {
			fields["lines["+strconv.Itoa(i)+"].account_id"] = "required"
			continue
		}
		lines = append(lines, model.CreditNoteLine{
			Description: l.Description,
			Quantity:    qty,
			UnitPrice:   price,
			AccountID:   l.AccountID,
			Amount:      qty * price / 100,
		})
	}

	if len(fields) > 0 {
		return fields, nil, 0
	}
	return nil, lines, taxAmount
}

// List handles GET /api/v1/credit-notes
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	notes, err := model.ListCreditNotes(h.DB, model.CreditNoteFilter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
	})
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list credit notes", nil)
		return
	}
	if notes == nil {
		notes = []model.CreditNote{}
	}

	total := len(notes)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}
	v1.WriteList(w, http.StatusOK, notes[start:end], page, total)
}

// Get handles GET /api/v1/credit-notes/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}
	cn, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}
	v1.WriteJSON(w, http.StatusOK, cn)
}

// Create handles POST /api/v1/credit-notes
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	var inp creditNoteInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, lines, taxAmount := validateInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	cn := &model.CreditNote{
		ContactID: inp.ContactID,
		InvoiceID: inp.InvoiceID,
		CNDate:    inp.CNDate,
		Reason:    inp.Reason,
		TaxAmount: taxAmount,
		Notes:     inp.Notes,
		CreatedBy: user.ID,
	}

	cnID, err := model.CreateCreditNote(h.DB, cn, lines)
	if err != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, err.Error(), nil)
		return
	}

	created, err := model.GetCreditNote(h.DB, cnID)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load credit note", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "credit_note.create",
		TargetType:  "credit_note",
		TargetID:    int64(cnID),
		TargetLabel: created.CNNumber,
		Metadata: map[string]any{
			"after": map[string]any{
				"contact_id": created.ContactID,
				"invoice_id": created.InvoiceID,
				"cn_date":    created.CNDate,
				"reason":     created.Reason,
				"total":      created.Total,
				"line_count": len(created.Lines),
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, created)
}

// Update handles PUT /api/v1/credit-notes/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}
	existing, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}
	if existing.Status != model.StatusDraft {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict,
			"can only edit draft credit notes (current: "+existing.Status+")", nil)
		return
	}

	var inp creditNoteInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, lines, taxAmount := validateInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	cn := &model.CreditNote{
		ID:        id,
		ContactID: inp.ContactID,
		InvoiceID: inp.InvoiceID,
		CNDate:    inp.CNDate,
		Reason:    inp.Reason,
		TaxAmount: taxAmount,
		Notes:     inp.Notes,
	}

	if err := model.UpdateCreditNote(h.DB, cn, lines); err != nil {
		if strings.Contains(err.Error(), "draft") {
			v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
			return
		}
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, err.Error(), nil)
		return
	}

	updated, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load credit note", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "credit_note.update",
		TargetType:  "credit_note",
		TargetID:    int64(id),
		TargetLabel: updated.CNNumber,
		Metadata: map[string]any{
			"before": map[string]any{
				"contact_id": existing.ContactID,
				"reason":     existing.Reason,
				"total":      existing.Total,
			},
			"after": map[string]any{
				"contact_id": updated.ContactID,
				"reason":     updated.Reason,
				"total":      updated.Total,
			},
		},
	})

	v1.WriteJSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /api/v1/credit-notes/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}
	existing, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}

	if err := model.DeleteCreditNote(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "credit_note.delete",
		TargetType:  "credit_note",
		TargetID:    int64(id),
		TargetLabel: existing.CNNumber,
		Metadata: map[string]any{
			"before": map[string]any{
				"contact_id": existing.ContactID,
				"status":     existing.Status,
				"total":      existing.Total,
			},
		},
	})

	w.WriteHeader(http.StatusNoContent)
}

// Issue handles POST /api/v1/credit-notes/{id}/issue
func (h *Handler) Issue(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}
	if _, err := model.GetCreditNote(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}

	if err := model.IssueCreditNote(h.DB, id, user.ID); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	updated, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load credit note", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "credit_note.issue",
		TargetType:  "credit_note",
		TargetID:    int64(id),
		TargetLabel: updated.CNNumber,
		Metadata: map[string]any{
			"after":      map[string]any{"status": updated.Status},
			"journal_id": updated.JournalID,
			"invoice_id": updated.InvoiceID,
		},
	})

	v1.WriteJSON(w, http.StatusOK, updated)
}

// Void handles POST /api/v1/credit-notes/{id}/void
func (h *Handler) Void(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}
	if _, err := model.GetCreditNote(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "credit note not found", nil)
		return
	}

	if err := model.VoidCreditNote(h.DB, id, user.ID); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	updated, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load credit note", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "credit_note.void",
		TargetType:  "credit_note",
		TargetID:    int64(id),
		TargetLabel: updated.CNNumber,
		Metadata: map[string]any{
			"after":      map[string]any{"status": updated.Status},
			"invoice_id": updated.InvoiceID,
		},
	})

	v1.WriteJSON(w, http.StatusOK, updated)
}
