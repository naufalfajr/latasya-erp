// Package invoices implements the /api/v1/invoices endpoints with
// idempotent lifecycle actions (create, update, send, payment).
package invoices

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	invoiceModule "github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/pdf"
)

type Handler struct {
	DB       *sql.DB
	Invoices *invoiceModule.Module
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

func parseInvoiceInput(inp *invoiceInput) (map[string]string, invoiceModule.Draft) {
	fields := map[string]string{}
	tax, err := parseIDR(inp.TaxAmount)
	if err != nil {
		fields["tax_amount"] = "invalid amount"
	}

	lines := make([]invoiceModule.DraftLine, 0, len(inp.Lines))
	for i, l := range inp.Lines {
		idx := strconv.Itoa(i)
		qty, err := parseQuantity(l.Quantity)
		if err != nil || qty <= 0 {
			fields["lines["+idx+"].quantity"] = "must be positive"
		}
		price, err := parseIDR(l.UnitPrice)
		if err != nil {
			fields["lines["+idx+"].unit_price"] = "must be positive"
		}
		lines = append(lines, invoiceModule.DraftLine{
			Description: l.Description,
			Quantity:    qty,
			UnitPrice:   price,
			AccountID:   l.AccountID,
		})
	}

	draft := invoiceModule.Draft{
		ContactID: inp.ContactID, InvoiceDate: inp.InvoiceDate, DueDate: inp.DueDate,
		TaxAmount: tax, Notes: inp.Notes, Lines: lines,
	}
	var validation *invoiceModule.ValidationError
	if errors.As(invoiceModule.ValidateDraft(draft), &validation) {
		for name, message := range validation.Fields {
			if fields[name] == "" {
				fields[name] = apiInvoiceValidationMessage(name, message)
			}
		}
	}
	if len(fields) == 0 {
		fields = nil
	}
	return fields, draft
}

func apiInvoiceValidationMessage(name, fallback string) string {
	switch {
	case name == "contact_id" || name == "invoice_date" || name == "due_date":
		return "required"
	case name == "lines":
		return "at least one line required"
	case strings.HasSuffix(name, ".description") || strings.HasSuffix(name, ".account_id"):
		return "required"
	case strings.HasSuffix(name, ".quantity") || strings.HasSuffix(name, ".unit_price"):
		return "must be positive"
	default:
		return fallback
	}
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
		Limit:  page.PerPage,
		Offset: page.Offset(),
	}

	total, err := model.CountInvoices(h.DB, filter)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list invoices", nil)
		return
	}

	invoices, err := model.ListInvoices(h.DB, filter)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list invoices", nil)
		return
	}
	if invoices == nil {
		invoices = []model.Invoice{}
	}

	v1.WriteList(w, http.StatusOK, invoices, page, total)
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

	fields, draft := parseInvoiceInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	created, err := h.Invoices.Create(r.Context(), invoiceModule.Actor{
		UserID: user.ID, CanManage: v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage),
	}, draft)
	if err != nil {
		writeModuleError(w, r, err, "failed to create invoice")
		return
	}

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
	user := auth.UserFromContext(r.Context())
	actor := invoiceModule.Actor{
		UserID: user.ID, CanManage: v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage),
	}
	if err := h.Invoices.CheckEditable(r.Context(), actor, id); err != nil {
		writeModuleError(w, r, err, "failed to update invoice")
		return
	}

	var inp invoiceInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, draft := parseInvoiceInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	updated, err := h.Invoices.Update(r.Context(), actor, id, draft)
	if err != nil {
		writeModuleError(w, r, err, "failed to update invoice")
		return
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": updated})
}

func writeModuleError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	var validation *invoiceModule.ValidationError
	var conflict *invoiceModule.ConflictError
	switch {
	case errors.Is(err, invoiceModule.ErrForbidden):
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, err.Error(), nil)
	case errors.Is(err, invoiceModule.ErrNotFound):
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, err.Error(), nil)
	case errors.As(err, &validation):
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, validation.Error(), validation.Fields)
	case errors.As(err, &conflict):
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, conflict.Error(), nil)
	default:
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, fallback, nil)
	}
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

// GenerateRecurring handles POST /api/v1/invoices/generate-recurring. It
// creates a draft invoice for every active customer from current contact pricing.
// Requires invoices.manage. Invoice date is today, due date is today + 10 days.
// Idempotency-Key is supported.
func (h *Handler) GenerateRecurring(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())

	now := time.Now()
	invoiceDate := now.Format("2006-01-02")
	dueDate := now.AddDate(0, 0, 10).Format("2006-01-02")

	result, err := model.GenerateRecurringInvoices(h.DB, invoiceDate, dueDate, user.ID)
	if err != nil {
		if errors.Is(err, model.ErrNoDefaultRevenueAccount) {
			v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, err.Error(), nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to generate recurring invoices", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:     "invoice.generate_recurring",
		TargetType: "invoice",
		Metadata: map[string]any{
			"invoice_date":       invoiceDate,
			"due_date":           dueDate,
			"effective_days":     result.EffectiveDays,
			"multiplier_percent": result.MultiplierPercent,
			"created":            result.Created,
			"skipped":            result.Skipped,
			"failed":             result.Failed,
			"created_invoices":   result.CreatedNumbers(),
		},
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": result})
}

type bulkDeleteInput struct {
	IDs []int `json:"ids"`
}

// BulkDelete handles POST /api/v1/invoices/bulk-delete. It deletes the draft
// invoices among the provided IDs; non-draft or unknown IDs are skipped and
// returned. Requires invoices.manage.
func (h *Handler) BulkDelete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}

	var inp bulkDeleteInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	if len(inp.IDs) == 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed",
			map[string]string{"ids": "at least one id required"})
		return
	}

	deleted, skipped, err := model.BulkDeleteDraftInvoices(h.DB, inp.IDs)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to delete invoices", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:     "invoice.bulk_delete",
		TargetType: "invoice",
		Metadata:   map[string]any{"deleted": deleted, "skipped": skipped},
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"deleted":          len(deleted),
			"deleted_invoices": deleted,
			"skipped":          skipped,
		},
	})
}

type bulkSendInput struct {
	IDs []int `json:"ids"`
}

// BulkSend handles POST /api/v1/invoices/bulk-send. It marks the draft invoices
// among the provided IDs as sent (posting each one's AR journal entry).
// Non-draft or unknown IDs are skipped. Requires invoices.manage.
func (h *Handler) BulkSend(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())

	var inp bulkSendInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	if len(inp.IDs) == 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed",
			map[string]string{"ids": "at least one id required"})
		return
	}

	res, err := model.BulkSendInvoices(h.DB, inp.IDs, user.ID)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to send invoices", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:     "invoice.bulk_send",
		TargetType: "invoice",
		Metadata:   map[string]any{"sent": res.Sent, "skipped": res.Skipped, "failed": res.Failed},
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"sent":          len(res.Sent),
			"sent_invoices": res.Sent,
			"skipped":       res.Skipped,
			"failed":        res.Failed,
		},
	})
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

// PDF handles GET /api/v1/invoices/{id}/pdf. It is read-only and not
// capability-gated, like the other invoice read endpoints. Note the rendered
// document embeds company-profile fields (bank account, NPWP) that the JSON
// endpoints do not return; gate this route before issuing down-scoped tokens
// (e.g. Telegram bot or third-party MCP).
func (h *Handler) PDF(w http.ResponseWriter, r *http.Request) {
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

	company, err := model.GetCompanyProfile(h.DB)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load company profile", nil)
		return
	}

	data, err := pdf.InvoicePDF(inv, company)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to generate pdf", nil)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", inv.InvoiceNumber+".pdf"))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}
