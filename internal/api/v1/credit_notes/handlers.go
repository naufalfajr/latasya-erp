// Package creditnotes implements the /api/v1/credit-notes endpoints.
package creditnotes

import (
	"errors"
	"net/http"
	"strconv"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	creditModule "github.com/naufal/latasya-erp/internal/creditnote"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ CreditNotes *creditModule.Module }
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

func validate(inp *creditNoteInput) (map[string]string, creditModule.Draft) {
	fields := map[string]string{}
	d := creditModule.Draft{ContactID: inp.ContactID, InvoiceID: inp.InvoiceID, Date: inp.CNDate, Reason: inp.Reason, Notes: inp.Notes}
	tax, err := v1.ParseIDR(inp.TaxAmount)
	if err != nil {
		fields["tax_amount"] = "invalid amount"
	}
	d.TaxAmount = tax
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
		d.Lines = append(d.Lines, creditModule.Line{Description: l.Description, Quantity: qty, UnitPrice: price, AccountID: l.AccountID})
	}
	if len(fields) > 0 {
		return fields, creditModule.Draft{}
	}
	return nil, d
}
func actor(r *http.Request) creditModule.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return creditModule.Actor{}
	}
	return creditModule.Actor{UserID: u.ID, CanManage: v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage)}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page := v1.ParsePage(r)
	result, err := h.CreditNotes.List(r.Context(), creditModule.Filter{Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("search"), Limit: page.PerPage, Offset: page.Offset()})
	if err != nil {
		v1.WriteError(w, r, 500, v1.CodeInternal, "failed to list credit notes", nil)
		return
	}
	v1.WriteList(w, 200, result.CreditNotes, page, result.Total)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		notFound(w, r)
		return
	}
	cn, err := h.CreditNotes.Get(r.Context(), id)
	if err != nil {
		notFound(w, r)
		return
	}
	v1.WriteJSON(w, 200, cn)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		forbidden(w, r)
		return
	}
	if auth.UserFromContext(r.Context()) == nil {
		unauthorized(w, r)
		return
	}
	var inp creditNoteInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, 400, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	fields, d := validate(&inp)
	if fields != nil {
		v1.WriteError(w, r, 422, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	created, err := h.CreditNotes.Create(r.Context(), actor(r), d)
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	v1.WriteJSON(w, 201, created)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		forbidden(w, r)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		notFound(w, r)
		return
	}
	if _, err = h.CreditNotes.Get(r.Context(), id); err != nil {
		notFound(w, r)
		return
	}
	var inp creditNoteInput
	if err = v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, 400, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	fields, d := validate(&inp)
	if fields != nil {
		v1.WriteError(w, r, 422, v1.CodeValidationFailed, "validation failed", fields)
		return
	}
	updated, err := h.CreditNotes.Update(r.Context(), actor(r), id, d)
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	v1.WriteJSON(w, 200, updated)
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		forbidden(w, r)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		notFound(w, r)
		return
	}
	if _, err = h.CreditNotes.Delete(r.Context(), actor(r), id); err != nil {
		writeModuleError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) Issue(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
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
	updated, err := h.CreditNotes.Issue(r.Context(), actor(r), id)
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	v1.WriteJSON(w, 200, updated)
}
func (h *Handler) Void(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
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
	updated, err := h.CreditNotes.Void(r.Context(), actor(r), id)
	if err != nil {
		writeModuleError(w, r, err)
		return
	}
	v1.WriteJSON(w, 200, updated)
}

func forbidden(w http.ResponseWriter, r *http.Request) {
	v1.WriteError(w, r, 403, v1.CodeForbidden, "invoices.manage capability required", nil)
}
func unauthorized(w http.ResponseWriter, r *http.Request) {
	v1.WriteError(w, r, 401, v1.CodeUnauthorized, "authentication required", nil)
}
func notFound(w http.ResponseWriter, r *http.Request) {
	v1.WriteError(w, r, 404, v1.CodeNotFound, "credit note not found", nil)
}
func writeModuleError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *creditModule.ValidationError
	var conflict *creditModule.ConflictError
	switch {
	case errors.Is(err, creditModule.ErrNotFound):
		notFound(w, r)
	case errors.Is(err, creditModule.ErrForbidden):
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
	mux.HandleFunc("GET /api/v1/credit-notes", h.List)
	mux.HandleFunc("GET /api/v1/credit-notes/{id}", h.Get)
	mux.Handle("POST /api/v1/credit-notes", idem(http.HandlerFunc(h.Create)))
	mux.Handle("PUT /api/v1/credit-notes/{id}", idem(http.HandlerFunc(h.Update)))
	mux.HandleFunc("DELETE /api/v1/credit-notes/{id}", h.Delete)
	mux.Handle("POST /api/v1/credit-notes/{id}/issue", idem(http.HandlerFunc(h.Issue)))
	mux.Handle("POST /api/v1/credit-notes/{id}/void", idem(http.HandlerFunc(h.Void)))
}
