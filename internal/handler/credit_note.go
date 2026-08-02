package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/auth"
	creditModule "github.com/naufal/latasya-erp/internal/creditnote"
	"github.com/naufal/latasya-erp/internal/model"
)

type creditNoteFormData struct {
	CreditNote      *model.CreditNote
	Lines           []model.CreditNoteLine
	Contacts        []model.Contact
	RevenueAccounts []model.Account
	Reasons         []reasonOption
	Errors          map[string]string
	IsEdit          bool
	SourceInvoice   *model.Invoice
}
type reasonOption struct{ Value, Label string }

var creditNoteReasons = []reasonOption{{model.CreditNoteReasonCancellation, "Cancellation"}, {model.CreditNoteReasonReturn, "Return"}, {model.CreditNoteReasonDiscount, "Discount"}, {model.CreditNoteReasonOther, "Other"}}

func (h *Handler) creditActor(r *http.Request) creditModule.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return creditModule.Actor{}
	}
	return creditModule.Actor{UserID: u.ID, CanManage: u.HasCapability(model.CapInvoicesManage)}
}
func creditDraft(cn *model.CreditNote, lines []model.CreditNoteLine) creditModule.Draft {
	d := creditModule.Draft{ContactID: cn.ContactID, InvoiceID: cn.InvoiceID, Date: cn.CNDate, Reason: cn.Reason, TaxAmount: cn.TaxAmount, Notes: cn.Notes, Lines: make([]creditModule.Line, len(lines))}
	for i, l := range lines {
		d.Lines[i] = creditModule.Line{Description: l.Description, Quantity: l.Quantity, UnitPrice: l.UnitPrice, AccountID: l.AccountID}
	}
	return d
}

func (h *Handler) ListCreditNotes(w http.ResponseWriter, r *http.Request) {
	f := creditModule.Filter{Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("search")}
	page := parsePage(r)
	f.Limit, f.Offset = listPageSize, (page-1)*listPageSize
	result, err := h.CreditNotes.List(r.Context(), f)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	pg := newPagination(page, result.Total)
	if pg.Page != page {
		f.Offset = pg.Offset()
		result, err = h.CreditNotes.List(r.Context(), f)
		if err != nil {
			http.Error(w, "Internal Server Error", 500)
			return
		}
	}
	h.render(w, r, "templates/credit_notes/index.html", "Credit Notes", map[string]any{"CreditNotes": result.CreditNotes, "Filter": f.Status, "Search": f.Search, "Pagination": newPageNav(pg, map[string]string{"status": f.Status, "search": f.Search})})
}
func (h *Handler) NewCreditNote(w http.ResponseWriter, r *http.Request) {
	fd, err := h.newCreditNoteFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	fd.CreditNote = &model.CreditNote{Reason: model.CreditNoteReasonCancellation}
	fd.Lines = []model.CreditNoteLine{{Quantity: 100}}
	if raw := r.URL.Query().Get("invoice_id"); raw != "" {
		if id, err := strconv.Atoi(raw); err == nil {
			if inv, err := h.Invoices.Get(r.Context(), id); err == nil {
				fd.SourceInvoice = inv
				fd.CreditNote.ContactID = inv.ContactID
				fd.CreditNote.InvoiceID = &inv.ID
				fd.CreditNote.TaxAmount = inv.TaxAmount
				lines := make([]model.CreditNoteLine, 0, len(inv.Lines))
				for _, l := range inv.Lines {
					lines = append(lines, model.CreditNoteLine{Description: l.Description, Quantity: l.Quantity, UnitPrice: l.UnitPrice, Amount: l.Amount, AccountID: l.AccountID})
				}
				if len(lines) > 0 {
					fd.Lines = lines
				}
			}
		}
	}
	h.render(w, r, "templates/credit_notes/form.html", "New Credit Note", fd, "templates/credit_notes/line_partial.html")
}
func (h *Handler) CreateCreditNote(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	cn, lines := parseCreditNote(r)
	created, err := h.CreditNotes.Create(r.Context(), h.creditActor(r), creditDraft(cn, lines))
	if err != nil {
		h.renderCreditForm(w, r, cn, lines, creditFormErrors(err), false)
		return
	}
	h.setFlash(w, "Credit note created")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", created.ID), 303)
}
func (h *Handler) ViewCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cn, err := h.CreditNotes.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, "templates/credit_notes/view.html", "Credit Note "+cn.CNNumber, map[string]any{"CreditNote": cn})
}
func (h *Handler) EditCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cn, err := h.CreditNotes.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if cn.Status != model.StatusDraft {
		h.setFlash(w, "Can only edit draft credit notes")
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), 303)
		return
	}
	fd, err := h.newCreditNoteFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	fd.CreditNote, fd.Lines, fd.IsEdit = cn, cn.Lines, true
	h.render(w, r, "templates/credit_notes/form.html", "Edit Credit Note", fd, "templates/credit_notes/line_partial.html")
}
func (h *Handler) UpdateCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = h.CreditNotes.Get(r.Context(), id); err != nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	cn, lines := parseCreditNote(r)
	cn.ID = id
	if _, err = h.CreditNotes.Update(r.Context(), h.creditActor(r), id, creditDraft(cn, lines)); err != nil {
		h.renderCreditForm(w, r, cn, lines, creditFormErrors(err), true)
		return
	}
	h.setFlash(w, "Credit note updated")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), 303)
}
func (h *Handler) IssueCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = h.CreditNotes.Issue(r.Context(), h.creditActor(r), id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		h.setFlash(w, "Credit note issued — journal entry posted")
	}
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), 303)
}
func (h *Handler) VoidCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = h.CreditNotes.Void(r.Context(), h.creditActor(r), id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		h.setFlash(w, "Credit note voided")
	}
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), 303)
}
func (h *Handler) DeleteCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err = h.CreditNotes.Delete(r.Context(), h.creditActor(r), id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), 303)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(200)
		return
	}
	h.setFlash(w, "Credit note deleted")
	http.Redirect(w, r, h.BasePath+"/credit-notes", 303)
}
func (h *Handler) CreditNoteLinePartial(w http.ResponseWriter, r *http.Request) {
	options, err := h.CreditNotes.Options(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	t, err := h.getTemplate("templates/credit_notes/line_partial.html")
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	t.ExecuteTemplate(w, "credit-note-line", map[string]any{"Accounts": options.RevenueAccounts})
}

func parseCreditNote(r *http.Request) (*model.CreditNote, []model.CreditNoteLine) {
	contact, _ := strconv.Atoi(r.FormValue("contact_id"))
	var invoiceID *int
	if raw := r.FormValue("invoice_id"); raw != "" {
		if id, err := strconv.Atoi(raw); err == nil && id > 0 {
			invoiceID = &id
		}
	}
	cn := &model.CreditNote{ContactID: contact, InvoiceID: invoiceID, CNDate: r.FormValue("cn_date"), Reason: r.FormValue("reason"), TaxAmount: parseIDR(r.FormValue("tax_amount")), Notes: r.FormValue("notes")}
	return cn, parseCreditNoteLines(r)
}
func parseCreditNoteLines(r *http.Request) []model.CreditNoteLine {
	descriptions, quantities, prices, accounts := r.Form["line_description"], r.Form["line_quantity"], r.Form["line_unit_price"], r.Form["line_account_id"]
	lines := []model.CreditNoteLine{}
	for i := range descriptions {
		desc := getIndex(descriptions, i)
		qty := parseQuantity(getIndex(quantities, i))
		if qty == 0 {
			qty = 100
		}
		price := parseIDR(getIndex(prices, i))
		account, _ := strconv.Atoi(getIndex(accounts, i))
		if desc == "" && price == 0 && account == 0 {
			continue
		}
		lines = append(lines, model.CreditNoteLine{Description: desc, Quantity: qty, UnitPrice: price, AccountID: account, Amount: qty * price / 100})
	}
	return lines
}
func creditFormErrors(err error) map[string]string {
	var validation *creditModule.ValidationError
	if !errors.As(err, &validation) {
		return map[string]string{"general": err.Error()}
	}
	return transportFormFields(validation.Fields)
}
func (h *Handler) newCreditNoteFormData(r *http.Request) (creditNoteFormData, error) {
	options, err := h.CreditNotes.Options(r.Context())
	if err != nil {
		return creditNoteFormData{}, err
	}
	return creditNoteFormData{Contacts: options.Contacts, RevenueAccounts: options.RevenueAccounts, Reasons: creditNoteReasons, Errors: map[string]string{}}, nil
}
func (h *Handler) renderCreditForm(w http.ResponseWriter, r *http.Request, cn *model.CreditNote, lines []model.CreditNoteLine, errs map[string]string, edit bool) {
	fd, err := h.newCreditNoteFormData(r)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	fd.CreditNote, fd.Lines, fd.Errors, fd.IsEdit = cn, lines, errs, edit
	title := "New Credit Note"
	if edit {
		title = "Edit Credit Note"
	}
	h.render(w, r, "templates/credit_notes/form.html", title, fd, "templates/credit_notes/line_partial.html")
}
