// Package invoices implements the /api/v1/invoices endpoints with
// idempotent lifecycle actions (create, update, send, payment).
package invoices

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

type Handler struct {
	DB *sql.DB
}

type lineInput struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	AccountID   int    `json:"account_id"`
}

type invoiceInput struct {
	ContactID   int         `json:"contact_id"`
	InvoiceDate string      `json:"invoice_date"`
	DueDate     string      `json:"due_date"`
	TaxAmount   string      `json:"tax_amount"`
	Notes       string      `json:"notes"`
	Lines       []lineInput `json:"lines"`
}

type paymentInput struct {
	Amount         string `json:"amount"`
	PaymentDate    string `json:"payment_date"`
	PaymentAccount int    `json:"payment_account"`
}

// parseIDR parses an integer-IDR string. Empty == 0. Negatives rejected.
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

// parseQuantity converts a decimal string like "1.50" into the
// integer ×100 representation used internally (150).
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
		f := parts[1]
		if len(f) > 2 {
			f = f[:2]
		}
		for len(f) < 2 {
			f += "0"
		}
		frac, err = strconv.Atoi(f)
		if err != nil || frac < 0 {
			return 0, errors.New("invalid quantity")
		}
	}
	return whole*100 + frac, nil
}

func validateInvoiceInput(inp *invoiceInput) (map[string]string, []model.InvoiceLine, int) {
	fields := map[string]string{}
	if inp.ContactID <= 0 {
		fields["contact_id"] = "required"
	}
	if strings.TrimSpace(inp.InvoiceDate) == "" {
		fields["invoice_date"] = "required"
	}
	if strings.TrimSpace(inp.DueDate) == "" {
		fields["due_date"] = "required"
	}
	if len(inp.Lines) == 0 {
		fields["lines"] = "at least one line required"
	}

	tax, err := parseIDR(inp.TaxAmount)
	if err != nil {
		fields["tax_amount"] = "invalid amount"
	}

	lines := make([]model.InvoiceLine, 0, len(inp.Lines))
	for i, l := range inp.Lines {
		idx := strconv.Itoa(i)
		if strings.TrimSpace(l.Description) == "" {
			fields["lines["+idx+"].description"] = "required"
		}
		qty, err := parseQuantity(l.Quantity)
		if err != nil || qty <= 0 {
			fields["lines["+idx+"].quantity"] = "must be positive"
			continue
		}
		price, err := parseIDR(l.UnitPrice)
		if err != nil || price <= 0 {
			fields["lines["+idx+"].unit_price"] = "must be positive"
			continue
		}
		if l.AccountID <= 0 {
			fields["lines["+idx+"].account_id"] = "required"
			continue
		}
		lines = append(lines, model.InvoiceLine{
			Description: l.Description,
			Quantity:    qty,
			UnitPrice:   price,
			AccountID:   l.AccountID,
		})
	}

	if len(fields) > 0 {
		return fields, nil, 0
	}
	return nil, lines, tax
}

// invoiceResponse wraps the model invoice with credit-note summary used
// by the Get endpoint.
type invoiceResponse struct {
	*model.Invoice
	CreditNotes []model.CreditNote `json:"credit_notes,omitempty"`
}

// List handles GET /api/v1/invoices.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	filter := model.InvoiceFilter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
	}

	invoices, err := model.ListInvoices(h.DB, filter)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list invoices", nil)
		return
	}
	if invoices == nil {
		invoices = []model.Invoice{}
	}

	total := len(invoices)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}

	v1.WriteList(w, http.StatusOK, invoices[start:end], page, total)
}

// Get handles GET /api/v1/invoices/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}

	inv, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}

	creditNotes, _ := model.ListCreditNotesForInvoice(h.DB, id)
	if creditNotes == nil {
		creditNotes = []model.CreditNote{}
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{
		"data": invoiceResponse{Invoice: inv, CreditNotes: creditNotes},
	})
}

// Create handles POST /api/v1/invoices. Requires invoices.manage capability.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())

	var inp invoiceInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, lines, tax := validateInvoiceInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	inv := &model.Invoice{
		ContactID:   inp.ContactID,
		InvoiceDate: inp.InvoiceDate,
		DueDate:     inp.DueDate,
		TaxAmount:   tax,
		Notes:       inp.Notes,
		CreatedBy:   user.ID,
	}

	id, err := model.CreateInvoice(h.DB, inv, lines)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create invoice", nil)
		return
	}

	created, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve created invoice", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "invoice.create",
		TargetType:  "invoice",
		TargetID:    int64(id),
		TargetLabel: created.InvoiceNumber,
		Metadata: map[string]any{
			"after": map[string]any{
				"contact_id":   created.ContactID,
				"invoice_date": created.InvoiceDate,
				"due_date":     created.DueDate,
				"total":        created.Total,
				"line_count":   len(created.Lines),
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": created})
}

// Update handles PUT /api/v1/invoices/{id}. Only draft invoices are editable.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}

	existing, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}
	if existing.Status != model.StatusDraft {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict,
			"can only edit draft invoices (current: "+existing.Status+")", nil)
		return
	}

	var inp invoiceInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, lines, tax := validateInvoiceInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	inv := &model.Invoice{
		ID:          id,
		ContactID:   inp.ContactID,
		InvoiceDate: inp.InvoiceDate,
		DueDate:     inp.DueDate,
		TaxAmount:   tax,
		Notes:       inp.Notes,
	}

	if err := model.UpdateInvoice(h.DB, inv, lines); err != nil {
		if strings.Contains(err.Error(), "can only edit") {
			v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update invoice", nil)
		return
	}

	updated, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve updated invoice", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "invoice.update",
		TargetType:  "invoice",
		TargetID:    int64(id),
		TargetLabel: updated.InvoiceNumber,
		Metadata: map[string]any{
			"before": map[string]any{"total": existing.Total},
			"after":  map[string]any{"total": updated.Total},
		},
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}

// Delete handles DELETE /api/v1/invoices/{id}. Only draft invoices may be deleted.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}

	existing, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}
	if existing.Status != model.StatusDraft {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict,
			"can only delete draft invoices (current: "+existing.Status+")", nil)
		return
	}

	if err := model.DeleteInvoice(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "invoice.delete",
		TargetType:  "invoice",
		TargetID:    int64(id),
		TargetLabel: existing.InvoiceNumber,
		Metadata: map[string]any{
			"before": map[string]any{
				"contact_id":   existing.ContactID,
				"invoice_date": existing.InvoiceDate,
				"total":        existing.Total,
			},
		},
	})

	w.WriteHeader(http.StatusNoContent)
}

// Send handles POST /api/v1/invoices/{id}/send.
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}

	existing, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}
	if existing.Status != model.StatusDraft {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict,
			"can only send draft invoices (current: "+existing.Status+")", nil)
		return
	}

	if err := model.SendInvoice(h.DB, id, user.ID); err != nil {
		if strings.Contains(err.Error(), "can only send") {
			v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to send invoice", nil)
		return
	}

	updated, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve invoice", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "invoice.send",
		TargetType:  "invoice",
		TargetID:    int64(id),
		TargetLabel: updated.InvoiceNumber,
		Metadata: map[string]any{
			"after":      map[string]any{"status": updated.Status},
			"journal_id": updated.JournalID,
		},
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}

// Payment handles POST /api/v1/invoices/{id}/payment.
func (h *Handler) Payment(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}

	existing, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "invoice not found", nil)
		return
	}

	var inp paymentInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields := map[string]string{}
	amount, err := parseIDR(inp.Amount)
	if err != nil || amount <= 0 {
		fields["amount"] = "must be positive"
	}
	if strings.TrimSpace(inp.PaymentDate) == "" {
		fields["payment_date"] = "required"
	}
	if inp.PaymentAccount <= 0 {
		fields["payment_account"] = "required"
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	if existing.Status == model.StatusDraft || existing.Status == "cancelled" || existing.Status == model.StatusPaid {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict,
			"cannot record payment for "+existing.Status+" invoice", nil)
		return
	}

	if err := model.RecordInvoicePayment(h.DB, id, amount, inp.PaymentDate, inp.PaymentAccount, user.ID); err != nil {
		if strings.Contains(err.Error(), "exceeds remaining") {
			v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, err.Error(),
				map[string]string{"amount": "exceeds remaining balance"})
			return
		}
		if strings.Contains(err.Error(), "cannot record payment") {
			v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to record payment", nil)
		return
	}

	updated, err := model.GetInvoice(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to retrieve invoice", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "invoice.payment",
		TargetType:  "invoice",
		TargetID:    int64(id),
		TargetLabel: updated.InvoiceNumber,
		Metadata: map[string]any{
			"amount":             amount,
			"payment_date":       inp.PaymentDate,
			"payment_account_id": inp.PaymentAccount,
			"status_after":       updated.Status,
		},
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}
