// Package bills implements the /api/v1/bills endpoints with full bill
// lifecycle support (draft → received → partial → paid).
package bills

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

// Handler holds shared dependencies for the bills API.
type Handler struct {
	DB *sql.DB
}

// lineInput is the JSON request body for a single bill line.
type lineInput struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	AccountID   int    `json:"account_id"`
}

// billInput is the JSON request body for Create and Update.
type billInput struct {
	ContactID int         `json:"contact_id"`
	BillDate  string      `json:"bill_date"`
	DueDate   string      `json:"due_date"`
	TaxAmount string      `json:"tax_amount"`
	Notes     string      `json:"notes"`
	Lines     []lineInput `json:"lines"`
}

type paymentInput struct {
	Amount         string `json:"amount"`
	PaymentDate    string `json:"payment_date"`
	PaymentAccount int    `json:"payment_account"`
}

// parseIDR parses a non-negative integer-IDR string. Empty → 0.
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

// parseQuantity parses a decimal quantity string into ×100 fixed-point.
// "1" → 100, "1.5" → 150, "1.25" → 125.
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

func validateBillInput(inp *billInput) (map[string]string, []model.BillLine, int) {
	fields := make(map[string]string)
	if inp.ContactID == 0 {
		fields["contact_id"] = "required"
	}
	if strings.TrimSpace(inp.BillDate) == "" {
		fields["bill_date"] = "required"
	}
	if strings.TrimSpace(inp.DueDate) == "" {
		fields["due_date"] = "required"
	}
	taxAmount, err := parseIDR(inp.TaxAmount)
	if err != nil {
		fields["tax_amount"] = "invalid amount"
	}
	if len(inp.Lines) == 0 {
		fields["lines"] = "at least one line required"
	}

	lines := make([]model.BillLine, 0, len(inp.Lines))
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
		lines = append(lines, model.BillLine{
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

// List handles GET /api/v1/bills
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	bills, err := model.ListBills(h.DB, model.BillFilter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
	})
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list bills", nil)
		return
	}
	if bills == nil {
		bills = []model.Bill{}
	}

	total := len(bills)
	start := page.Offset()
	if start > total {
		start = total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}
	v1.WriteList(w, http.StatusOK, bills[start:end], page, total)
}

// Get handles GET /api/v1/bills/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}
	b, err := model.GetBill(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}
	v1.WriteJSON(w, http.StatusOK, b)
}

// Create handles POST /api/v1/bills
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "bills.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	var inp billInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, lines, taxAmount := validateBillInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	b := &model.Bill{
		ContactID: inp.ContactID,
		BillDate:  inp.BillDate,
		DueDate:   inp.DueDate,
		TaxAmount: taxAmount,
		Notes:     inp.Notes,
		CreatedBy: user.ID,
	}

	billID, err := model.CreateBill(h.DB, b, lines)
	if err != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, err.Error(), nil)
		return
	}

	created, err := model.GetBill(h.DB, billID)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load created bill", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "bill.create",
		TargetType:  "bill",
		TargetID:    int64(billID),
		TargetLabel: created.BillNumber,
		Metadata: map[string]any{
			"after": map[string]any{
				"contact_id": created.ContactID,
				"bill_date":  created.BillDate,
				"due_date":   created.DueDate,
				"tax_amount": created.TaxAmount,
				"total":      created.Total,
				"line_count": len(created.Lines),
			},
		},
	})

	v1.WriteJSON(w, http.StatusCreated, created)
}

// Update handles PUT /api/v1/bills/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "bills.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}

	existing, err := model.GetBill(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}
	if existing.Status != "draft" {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict,
			"can only edit draft bills (current: "+existing.Status+")", nil)
		return
	}

	var inp billInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields, lines, taxAmount := validateBillInput(&inp)
	if fields != nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	b := &model.Bill{
		ID:        id,
		ContactID: inp.ContactID,
		BillDate:  inp.BillDate,
		DueDate:   inp.DueDate,
		TaxAmount: taxAmount,
		Notes:     inp.Notes,
	}

	if err := model.UpdateBill(h.DB, b, lines); err != nil {
		if strings.Contains(err.Error(), "draft") {
			v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
			return
		}
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, err.Error(), nil)
		return
	}

	updated, err := model.GetBill(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load updated bill", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "bill.update",
		TargetType:  "bill",
		TargetID:    int64(id),
		TargetLabel: updated.BillNumber,
		Metadata: map[string]any{
			"before": map[string]any{
				"contact_id": existing.ContactID,
				"bill_date":  existing.BillDate,
				"due_date":   existing.DueDate,
				"tax_amount": existing.TaxAmount,
				"total":      existing.Total,
			},
			"after": map[string]any{
				"contact_id": updated.ContactID,
				"bill_date":  updated.BillDate,
				"due_date":   updated.DueDate,
				"tax_amount": updated.TaxAmount,
				"total":      updated.Total,
			},
		},
	})

	v1.WriteJSON(w, http.StatusOK, updated)
}

// Delete handles DELETE /api/v1/bills/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "bills.manage capability required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}

	existing, err := model.GetBill(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}

	if err := model.DeleteBill(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "bill.delete",
		TargetType:  "bill",
		TargetID:    int64(id),
		TargetLabel: existing.BillNumber,
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

// Receive handles POST /api/v1/bills/{id}/receive — creates the AP journal entry.
func (h *Handler) Receive(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "bills.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}
	if _, err := model.GetBill(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}

	if err := model.ReceiveBill(h.DB, id, user.ID); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	updated, err := model.GetBill(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load bill", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "bill.receive",
		TargetType:  "bill",
		TargetID:    int64(id),
		TargetLabel: updated.BillNumber,
		Metadata: map[string]any{
			"after":      map[string]any{"status": updated.Status},
			"journal_id": updated.JournalID,
		},
	})

	v1.WriteJSON(w, http.StatusOK, updated)
}

// Payment handles POST /api/v1/bills/{id}/payment
func (h *Handler) Payment(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "bills.manage capability required", nil)
		return
	}
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
		return
	}
	if _, err := model.GetBill(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "bill not found", nil)
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
		fields["amount"] = "must be a positive integer-IDR string"
	}
	if strings.TrimSpace(inp.PaymentDate) == "" {
		fields["payment_date"] = "required"
	}
	if inp.PaymentAccount == 0 {
		fields["payment_account"] = "required"
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	if err := model.RecordBillPayment(h.DB, id, amount, inp.PaymentDate, inp.PaymentAccount, user.ID); err != nil {
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, err.Error(), nil)
		return
	}

	updated, err := model.GetBill(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load bill", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "bill.payment",
		TargetType:  "bill",
		TargetID:    int64(id),
		TargetLabel: updated.BillNumber,
		Metadata: map[string]any{
			"amount":             amount,
			"payment_date":       inp.PaymentDate,
			"payment_account_id": inp.PaymentAccount,
			"status_after":       updated.Status,
		},
	})

	v1.WriteJSON(w, http.StatusOK, updated)
}
