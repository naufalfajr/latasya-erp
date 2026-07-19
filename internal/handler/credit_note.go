package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
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
	// SourceInvoice is set when creating from an invoice's "Create Credit Note"
	// pre-fill button so the form can show the link explicitly.
	SourceInvoice *model.Invoice
}

type reasonOption struct {
	Value string
	Label string
}

var creditNoteReasons = []reasonOption{
	{Value: model.CreditNoteReasonCancellation, Label: "Cancellation"},
	{Value: model.CreditNoteReasonReturn, Label: "Return"},
	{Value: model.CreditNoteReasonDiscount, Label: "Discount"},
	{Value: model.CreditNoteReasonOther, Label: "Other"},
}

func (h *Handler) ListCreditNotes(w http.ResponseWriter, r *http.Request) {
	f := model.CreditNoteFilter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
	}
	total, err := model.CountCreditNotes(h.DB, f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	pg := newPagination(parsePage(r), total)
	f.Limit, f.Offset = pg.PageSize, pg.Offset()

	notes, err := model.ListCreditNotes(h.DB, f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, "templates/credit_notes/index.html", "Credit Notes", map[string]any{
		"CreditNotes": notes,
		"Filter":      f.Status,
		"Search":      f.Search,
		"Pagination":  newPageNav(pg, map[string]string{"status": f.Status, "search": f.Search}),
	})
}

func (h *Handler) NewCreditNote(w http.ResponseWriter, r *http.Request) {
	fd := h.newCreditNoteFormData()
	fd.CreditNote = &model.CreditNote{Reason: model.CreditNoteReasonCancellation}
	fd.Lines = []model.CreditNoteLine{{Quantity: 100}}

	// Pre-fill from an invoice (?invoice_id=N) — typically the "Create
	// Credit Note" button on the invoice view page.
	if invIDStr := r.URL.Query().Get("invoice_id"); invIDStr != "" {
		if invID, err := strconv.Atoi(invIDStr); err == nil {
			if inv, err := model.GetInvoice(h.DB, invID); err == nil {
				fd.SourceInvoice = inv
				fd.CreditNote.ContactID = inv.ContactID
				fd.CreditNote.InvoiceID = &inv.ID
				fd.CreditNote.TaxAmount = inv.TaxAmount
				lines := make([]model.CreditNoteLine, 0, len(inv.Lines))
				for _, il := range inv.Lines {
					lines = append(lines, model.CreditNoteLine{
						Description: il.Description,
						Quantity:    il.Quantity,
						UnitPrice:   il.UnitPrice,
						Amount:      il.Amount,
						AccountID:   il.AccountID,
					})
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
	user := auth.UserFromContext(r.Context())

	contactID, _ := strconv.Atoi(r.FormValue("contact_id"))
	taxAmount := parseIDR(r.FormValue("tax_amount"))
	var invoiceID *int
	if v := r.FormValue("invoice_id"); v != "" {
		if id, err := strconv.Atoi(v); err == nil && id > 0 {
			invoiceID = &id
		}
	}

	cn := &model.CreditNote{
		ContactID: contactID,
		InvoiceID: invoiceID,
		CNDate:    r.FormValue("cn_date"),
		Reason:    r.FormValue("reason"),
		TaxAmount: taxAmount,
		Notes:     r.FormValue("notes"),
		CreatedBy: user.ID,
	}
	lines := parseCreditNoteLines(r)
	errors := validateCreditNote(cn, lines)

	if len(errors) > 0 {
		fd := h.newCreditNoteFormData()
		fd.CreditNote = cn
		fd.Lines = lines
		fd.Errors = errors
		h.render(w, r, "templates/credit_notes/form.html", "New Credit Note", fd, "templates/credit_notes/line_partial.html")
		return
	}

	cnID, err := model.CreateCreditNote(h.DB, cn, lines)
	if err != nil {
		fd := h.newCreditNoteFormData()
		fd.CreditNote = cn
		fd.Lines = lines
		fd.Errors = map[string]string{"general": err.Error()}
		h.render(w, r, "templates/credit_notes/form.html", "New Credit Note", fd, "templates/credit_notes/line_partial.html")
		return
	}

	created, _ := model.GetCreditNote(h.DB, cnID)
	cnNumber := ""
	total := 0
	if created != nil {
		cnNumber = created.CNNumber
		total = created.Total
	}
	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "credit_note.create",
		TargetType:  "credit_note",
		TargetID:    int64(cnID),
		TargetLabel: cnNumber,
		Metadata: map[string]any{
			"after": map[string]any{
				"contact_id": cn.ContactID,
				"invoice_id": cn.InvoiceID,
				"cn_date":    cn.CNDate,
				"reason":     cn.Reason,
				"total":      total,
				"line_count": len(lines),
			},
		},
	})

	h.setFlash(w, "Credit note created")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", cnID), http.StatusSeeOther)
}

func (h *Handler) ViewCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cn, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, "templates/credit_notes/view.html", "Credit Note "+cn.CNNumber, map[string]any{
		"CreditNote": cn,
	})
}

func (h *Handler) EditCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cn, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if cn.Status != model.StatusDraft {
		h.setFlash(w, "Can only edit draft credit notes")
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), http.StatusSeeOther)
		return
	}
	fd := h.newCreditNoteFormData()
	fd.CreditNote = cn
	fd.Lines = cn.Lines
	fd.IsEdit = true
	h.render(w, r, "templates/credit_notes/form.html", "Edit Credit Note", fd, "templates/credit_notes/line_partial.html")
}

func (h *Handler) UpdateCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	old, err := model.GetCreditNote(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	r.ParseForm()
	contactID, _ := strconv.Atoi(r.FormValue("contact_id"))
	taxAmount := parseIDR(r.FormValue("tax_amount"))
	var invoiceID *int
	if v := r.FormValue("invoice_id"); v != "" {
		if pid, perr := strconv.Atoi(v); perr == nil && pid > 0 {
			invoiceID = &pid
		}
	}

	cn := &model.CreditNote{
		ID:        id,
		ContactID: contactID,
		InvoiceID: invoiceID,
		CNDate:    r.FormValue("cn_date"),
		Reason:    r.FormValue("reason"),
		TaxAmount: taxAmount,
		Notes:     r.FormValue("notes"),
	}
	lines := parseCreditNoteLines(r)
	errors := validateCreditNote(cn, lines)

	if len(errors) > 0 {
		fd := h.newCreditNoteFormData()
		fd.CreditNote = cn
		fd.Lines = lines
		fd.Errors = errors
		fd.IsEdit = true
		h.render(w, r, "templates/credit_notes/form.html", "Edit Credit Note", fd, "templates/credit_notes/line_partial.html")
		return
	}

	if err := model.UpdateCreditNote(h.DB, cn, lines); err != nil {
		fd := h.newCreditNoteFormData()
		fd.CreditNote = cn
		fd.Lines = lines
		fd.Errors = map[string]string{"general": err.Error()}
		fd.IsEdit = true
		h.render(w, r, "templates/credit_notes/form.html", "Edit Credit Note", fd, "templates/credit_notes/line_partial.html")
		return
	}

	updated, _ := model.GetCreditNote(h.DB, id)
	oldFields := map[string]any{
		"contact_id": old.ContactID, "invoice_id": old.InvoiceID, "cn_date": old.CNDate,
		"reason": old.Reason, "tax_amount": old.TaxAmount, "notes": old.Notes, "total": old.Total,
	}
	newFields := map[string]any{
		"contact_id": cn.ContactID, "invoice_id": cn.InvoiceID, "cn_date": cn.CNDate,
		"reason": cn.Reason, "tax_amount": cn.TaxAmount, "notes": cn.Notes,
	}
	if updated != nil {
		newFields["total"] = updated.Total
	}
	metadata := audit.Diff(oldFields, newFields,
		[]string{"contact_id", "invoice_id", "cn_date", "reason", "tax_amount", "notes", "total"})
	if metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action: "credit_note.update", TargetType: "credit_note",
			TargetID: int64(id), TargetLabel: old.CNNumber, Metadata: metadata,
		})
	}

	h.setFlash(w, "Credit note updated")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), http.StatusSeeOther)
}

func (h *Handler) IssueCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user := auth.UserFromContext(r.Context())
	if err := model.IssueCreditNote(h.DB, id, user.ID); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		if cn, err := model.GetCreditNote(h.DB, id); err == nil {
			audit.Log(r.Context(), h.DB, audit.Event{
				Action: "credit_note.issue", TargetType: "credit_note",
				TargetID: int64(id), TargetLabel: cn.CNNumber,
				Metadata: map[string]any{
					"after":      map[string]any{"status": cn.Status},
					"journal_id": cn.JournalID,
					"invoice_id": cn.InvoiceID,
				},
			})
		}
		h.setFlash(w, "Credit note issued — journal entry posted")
	}
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), http.StatusSeeOther)
}

func (h *Handler) VoidCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user := auth.UserFromContext(r.Context())
	if err := model.VoidCreditNote(h.DB, id, user.ID); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		if cn, err := model.GetCreditNote(h.DB, id); err == nil {
			audit.Log(r.Context(), h.DB, audit.Event{
				Action: "credit_note.void", TargetType: "credit_note",
				TargetID: int64(id), TargetLabel: cn.CNNumber,
				Metadata: map[string]any{
					"after":      map[string]any{"status": cn.Status},
					"invoice_id": cn.InvoiceID,
				},
			})
		}
		h.setFlash(w, "Credit note voided")
	}
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	existing, _ := model.GetCreditNote(h.DB, id)
	if err := model.DeleteCreditNote(h.DB, id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/credit-notes/%d", id), http.StatusSeeOther)
		return
	}
	if existing != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action: "credit_note.delete", TargetType: "credit_note",
			TargetID: int64(id), TargetLabel: existing.CNNumber,
			Metadata: map[string]any{
				"before": map[string]any{
					"contact_id": existing.ContactID,
					"invoice_id": existing.InvoiceID,
					"cn_date":    existing.CNDate,
					"status":     existing.Status,
					"total":      existing.Total,
				},
			},
		})
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.setFlash(w, "Credit note deleted")
	http.Redirect(w, r, h.BasePath+"/credit-notes", http.StatusSeeOther)
}

// CreditNoteLinePartial returns one new blank credit note line row for HTMX.
func (h *Handler) CreditNoteLinePartial(w http.ResponseWriter, r *http.Request) {
	active := true
	accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "revenue", IsActive: &active})

	t, err := h.getTemplate("templates/credit_notes/line_partial.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	t.ExecuteTemplate(w, "credit-note-line", map[string]any{"Accounts": accounts})
}

func parseCreditNoteLines(r *http.Request) []model.CreditNoteLine {
	descriptions := r.Form["line_description"]
	quantities := r.Form["line_quantity"]
	unitPrices := r.Form["line_unit_price"]
	accountIDs := r.Form["line_account_id"]

	var lines []model.CreditNoteLine
	for i := range descriptions {
		desc := getIndex(descriptions, i)
		qty := parseQuantity(getIndex(quantities, i))
		if qty == 0 {
			qty = 100
		}
		price := parseIDR(getIndex(unitPrices, i))
		accountID, _ := strconv.Atoi(getIndex(accountIDs, i))
		if desc == "" && price == 0 && accountID == 0 {
			continue
		}
		lines = append(lines, model.CreditNoteLine{
			Description: desc, Quantity: qty, UnitPrice: price,
			AccountID: accountID, Amount: qty * price / 100,
		})
	}
	return lines
}

func validateCreditNote(cn *model.CreditNote, lines []model.CreditNoteLine) map[string]string {
	errors := make(map[string]string)
	if cn.ContactID == 0 {
		errors["contact_id"] = "Customer is required"
	}
	if cn.CNDate == "" {
		errors["cn_date"] = "Date is required"
	}
	if cn.Reason == "" {
		errors["reason"] = "Reason is required"
	}
	if cn.Reason != "" {
		validReasons := map[string]bool{
			model.CreditNoteReasonCancellation: true,
			model.CreditNoteReasonReturn:       true,
			model.CreditNoteReasonDiscount:     true,
			model.CreditNoteReasonOther:        true,
		}
		if !validReasons[cn.Reason] {
			errors["reason"] = "Invalid reason"
		}
	}
	if len(lines) == 0 {
		errors["lines"] = "At least one line item is required"
	}
	for i, l := range lines {
		if l.Description == "" {
			errors[fmt.Sprintf("line_%d_desc", i)] = "Description required"
		}
		if l.UnitPrice <= 0 {
			errors[fmt.Sprintf("line_%d_price", i)] = "Price required"
		}
		if l.AccountID == 0 {
			errors[fmt.Sprintf("line_%d_account", i)] = "Account required"
		}
	}
	return errors
}

func (h *Handler) newCreditNoteFormData() creditNoteFormData {
	active := true
	contacts, _ := model.ListContacts(h.DB, model.ContactFilter{Type: "customer", IsActive: &active})
	revenueAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "revenue", IsActive: &active})

	return creditNoteFormData{
		Contacts:        contacts,
		RevenueAccounts: revenueAccounts,
		Reasons:         creditNoteReasons,
		Errors:          make(map[string]string),
	}
}
