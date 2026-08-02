// Package bills implements the /api/v1/bills endpoints.
package bills

import (
	"errors"
	"net/http"
	"strconv"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	billModule "github.com/naufal/latasya-erp/internal/bill"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Bills *billModule.Module }

type lineInput struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	AccountID   int    `json:"account_id"`
}
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

func validate(inp *billInput) (map[string]string, billModule.Draft) {
	fields := map[string]string{}
	draft := billModule.Draft{ContactID: inp.ContactID, BillDate: inp.BillDate, DueDate: inp.DueDate, Notes: inp.Notes}
	tax, err := v1.ParseIDR(inp.TaxAmount)
	if err != nil {
		fields["tax_amount"] = "invalid amount"
	}
	draft.TaxAmount = tax
	for i, l := range inp.Lines {
		key := "lines[" + strconv.Itoa(i) + "]"
		qty, qerr := v1.ParseQuantity(l.Quantity)
		if qerr != nil {
			fields[key+".quantity"] = "invalid quantity"
			continue
		}
		if qty == 0 {
			qty = 100
		}
		price, perr := v1.ParseIDR(l.UnitPrice)
		if perr != nil {
			fields[key+".unit_price"] = "invalid amount"
			continue
		}
		draft.Lines = append(draft.Lines, billModule.Line{Description: l.Description, Quantity: qty, UnitPrice: price, AccountID: l.AccountID})
	}
	if len(fields) > 0 {
		return fields, billModule.Draft{}
	}
	return nil, draft
}
func actor(r *http.Request) billModule.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return billModule.Actor{}
	}
	return billModule.Actor{UserID: u.ID, CanManage: v1.HasEffectiveCapability(r.Context(), model.CapBillsManage)}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	result, err := h.Bills.List(r.Context(), billModule.Filter{Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("search"), Limit: page.PerPage, Offset: page.Offset()})
	if err != nil {
		v1.WriteError(w, r, 500, v1.CodeInternal, "failed to list bills", nil)
		return
	}
	v1.WriteList(w, 200, result.Bills, page, result.Total)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		notFound(w, r)
		return
	}
	b, err := h.Bills.Get(r.Context(), id)
	if err != nil {
		notFound(w, r)
		return
	}
	v1.WriteJSON(w, 200, b)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		forbidden(w, r)
		return
	}
	if auth.UserFromContext(r.Context()) == nil {
		unauthorized(w, r)
		return
	}
	var inp billInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, 400, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	fields, d := validate(&inp)
	if fields != nil {
		v1.WriteError(w, r, 422, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	created, err := h.Bills.Create(r.Context(), actor(r), d)
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	v1.WriteJSON(w, 201, created)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		forbidden(w, r)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		notFound(w, r)
		return
	}
	if _, err = h.Bills.Get(r.Context(), id); err != nil {
		notFound(w, r)
		return
	}
	var inp billInput
	if err = v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, 400, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	fields, d := validate(&inp)
	if fields != nil {
		v1.WriteError(w, r, 422, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	updated, err := h.Bills.Update(r.Context(), actor(r), id, d)
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	v1.WriteJSON(w, 200, updated)
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		forbidden(w, r)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		notFound(w, r)
		return
	}
	if _, err = h.Bills.Delete(r.Context(), actor(r), id); err != nil {
		writeModuleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) Receive(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		forbidden(w, r)
		return
	}
	if auth.UserFromContext(r.Context()) == nil {
		unauthorized(w, r)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		notFound(w, r)
		return
	}
	updated, err := h.Bills.Receive(r.Context(), actor(r), id)
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	v1.WriteJSON(w, 200, updated)
}
func (h *Handler) Payment(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapBillsManage) {
		forbidden(w, r)
		return
	}
	if auth.UserFromContext(r.Context()) == nil {
		unauthorized(w, r)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		notFound(w, r)
		return
	}
	if _, err = h.Bills.Get(r.Context(), id); err != nil {
		notFound(w, r)
		return
	}
	var inp paymentInput
	if err = v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, 400, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	fields := map[string]string{}
	amount, err := v1.ParseIDR(inp.Amount)
	if err != nil {
		fields["amount"] = "must be an integer-IDR string"
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, 422, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	updated, err := h.Bills.RecordPayment(r.Context(), actor(r), id, billModule.Payment{Amount: amount, PaymentDate: inp.PaymentDate, PaymentAccount: inp.PaymentAccount})
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	v1.WriteJSON(w, 200, updated)
}

func forbidden(w http.ResponseWriter, r *http.Request) {
	v1.WriteError(w, r, 403, v1.CodeForbidden, "bills.manage capability required", nil)
}
func unauthorized(w http.ResponseWriter, r *http.Request) {
	v1.WriteError(w, r, 401, v1.CodeUnauthorized, "authentication required", nil)
}
func notFound(w http.ResponseWriter, r *http.Request) {
	v1.WriteError(w, r, 404, v1.CodeNotFound, "bill not found", nil)
}
func writeModuleError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *billModule.ValidationError
	var conflict *billModule.ConflictError
	switch {
	case errors.Is(err, billModule.ErrNotFound):
		notFound(w, r)
	case errors.Is(err, billModule.ErrForbidden):
		forbidden(w, r)
	case errors.As(err, &validation):
		v1.WriteError(w, r, 422, v1.CodeValidationFailed, validation.Error(), validation.Fields)
	case errors.As(err, &conflict):
		v1.WriteError(w, r, 409, v1.CodeConflict, conflict.Error(), nil)
	default:
		v1.WriteError(w, r, 500, v1.CodeInternal, "internal server error", nil)
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, idem func(http.Handler) http.Handler) {
	mux.HandleFunc("GET /api/v1/bills", h.List)
	mux.HandleFunc("GET /api/v1/bills/{id}", h.Get)
	mux.Handle("POST /api/v1/bills", idem(http.HandlerFunc(h.Create)))
	mux.Handle("PUT /api/v1/bills/{id}", idem(http.HandlerFunc(h.Update)))
	mux.HandleFunc("DELETE /api/v1/bills/{id}", h.Delete)
	mux.Handle("POST /api/v1/bills/{id}/receive", idem(http.HandlerFunc(h.Receive)))
	mux.Handle("POST /api/v1/bills/{id}/payment", idem(http.HandlerFunc(h.Payment)))
}
