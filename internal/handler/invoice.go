package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/naufal/latasya-erp/internal/auth"
	invoiceModule "github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/pdf"
)

type invoiceFormData struct {
	Invoice                      *model.Invoice
	Lines                        []model.InvoiceLine
	Contacts                     []model.Contact
	RevenueAccounts              []model.Account
	DefaultRevenueAccountID      int
	RecurringDescriptionTemplate string
	Errors                       map[string]string
	IsEdit                       bool
}

func (h *Handler) ListInvoices(w http.ResponseWriter, r *http.Request) {
	f := invoiceModule.Filter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
	}

	total, err := h.Invoices.Count(r.Context(), f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	pg := newPagination(parsePage(r), total)
	f.Limit, f.Offset = pg.PageSize, pg.Offset()

	invoices, err := h.Invoices.Find(r.Context(), f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/invoices/index.html", "Invoices", map[string]any{
		"Invoices":   invoices,
		"Filter":     f.Status,
		"Search":     f.Search,
		"Pagination": newPageNav(pg, map[string]string{"status": f.Status, "search": f.Search}),
	})
}

func (h *Handler) NewInvoice(w http.ResponseWriter, r *http.Request) {
	fd, err := h.newInvoiceFormData(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	fd.Invoice = &model.Invoice{}
	fd.Lines = []model.InvoiceLine{{Quantity: 100}} // 1 empty line, qty=1.00
	h.render(w, r, "templates/invoices/form.html", "New Invoice", fd, "templates/invoices/line_partial.html")
}

func (h *Handler) CreateInvoice(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	user := auth.UserFromContext(r.Context())

	contactID, _ := strconv.Atoi(r.FormValue("contact_id"))
	taxAmount := parseIDR(r.FormValue("tax_amount"))

	inv := &model.Invoice{
		ContactID:   contactID,
		InvoiceDate: r.FormValue("invoice_date"),
		DueDate:     r.FormValue("due_date"),
		TaxAmount:   taxAmount,
		Notes:       r.FormValue("notes"),
	}

	lines := parseInvoiceLines(r)
	created, err := h.Invoices.Create(r.Context(), invoiceModule.Actor{
		UserID: user.ID, CanManage: user.HasCapability(model.CapInvoicesManage),
	}, invoiceModule.Draft{
		ContactID: inv.ContactID, InvoiceDate: inv.InvoiceDate, DueDate: inv.DueDate,
		TaxAmount: inv.TaxAmount, Notes: inv.Notes, Lines: invoiceDraftLines(lines),
	})
	if err != nil {
		fd, formErr := h.newInvoiceFormData(r.Context())
		if formErr != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		fd.Invoice = inv
		fd.Lines = lines
		fd.Errors = invoiceFormErrors(err, lines)
		h.render(w, r, "templates/invoices/form.html", "New Invoice", fd, "templates/invoices/line_partial.html")
		return
	}

	h.setFlash(w, "Invoice created successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", created.ID), http.StatusSeeOther)
}

func (h *Handler) GenerateRecurringInvoices(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	now := time.Now()
	invoiceDate := now.Format("2006-01-02")
	dueDate := now.AddDate(0, 0, 10).Format("2006-01-02")

	result, err := h.Invoices.GenerateRecurring(r.Context(), invoiceModule.Actor{UserID: user.ID, CanManage: user.HasCapability(model.CapInvoicesManage)}, invoiceDate, dueDate)
	if err != nil {
		h.setFlash(w, "Error generating invoices: "+err.Error())
		http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Generated %d draft invoice(s). Skipped %d customer(s).", result.Created, result.Skipped)
	if result.Failed > 0 {
		msg += fmt.Sprintf(" Failed %d.", result.Failed)
	}
	h.setFlash(w, msg)
	http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
}

func (h *Handler) BulkDeleteInvoices(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var ids []int
	for _, s := range r.Form["ids"] {
		if id, err := strconv.Atoi(s); err == nil {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		h.setFlash(w, "No invoices selected")
		http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
		return
	}

	user := auth.UserFromContext(r.Context())
	result, err := h.Invoices.BulkDelete(r.Context(), invoiceModule.Actor{UserID: user.ID, CanManage: user.HasCapability(model.CapInvoicesManage)}, ids)
	if err != nil {
		h.setFlash(w, "Error deleting invoices: "+err.Error())
		http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Deleted %d draft invoice(s).", len(result.Deleted))
	if len(result.Skipped) > 0 {
		msg += fmt.Sprintf(" Skipped %d (not draft).", len(result.Skipped))
	}
	h.setFlash(w, msg)
	http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
}

func (h *Handler) BulkSendInvoices(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var ids []int
	for _, s := range r.Form["ids"] {
		if id, err := strconv.Atoi(s); err == nil {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		h.setFlash(w, "No invoices selected")
		http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
		return
	}

	user := auth.UserFromContext(r.Context())
	res, err := h.Invoices.BulkSend(r.Context(), invoiceModule.Actor{UserID: user.ID, CanManage: user.HasCapability(model.CapInvoicesManage)}, ids)
	if err != nil {
		h.setFlash(w, "Error sending invoices: "+err.Error())
		http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Marked %d invoice(s) as sent (journal entries posted).", len(res.Sent))
	if len(res.Skipped) > 0 {
		msg += fmt.Sprintf(" Skipped %d (not draft).", len(res.Skipped))
	}
	if len(res.Failed) > 0 {
		msg += fmt.Sprintf(" Failed %d.", len(res.Failed))
	}
	h.setFlash(w, msg)
	http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
}

func (h *Handler) ViewInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	detail, err := h.Invoices.View(r.Context(), id)
	if err != nil {
		if errors.Is(err, invoiceModule.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	h.render(w, r, "templates/invoices/view.html", "Invoice "+detail.Invoice.InvoiceNumber, map[string]any{
		"Invoice":       detail.Invoice,
		"AssetAccounts": detail.AssetAccounts,
		"CreditNotes":   detail.CreditNotes,
	})
}

func (h *Handler) EditInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	inv, err := h.Invoices.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, invoiceModule.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	if inv.Status != "draft" {
		h.setFlash(w, "Can only edit draft invoices")
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
		return
	}

	fd, err := h.newInvoiceFormData(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	fd.Invoice = inv
	fd.Lines = inv.Lines
	fd.IsEdit = true
	h.render(w, r, "templates/invoices/form.html", "Edit Invoice", fd, "templates/invoices/line_partial.html")
}

func (h *Handler) UpdateInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	r.ParseForm()
	user := auth.UserFromContext(r.Context())
	contactID, _ := strconv.Atoi(r.FormValue("contact_id"))
	taxAmount := parseIDR(r.FormValue("tax_amount"))

	inv := &model.Invoice{
		ID:          id,
		ContactID:   contactID,
		InvoiceDate: r.FormValue("invoice_date"),
		DueDate:     r.FormValue("due_date"),
		TaxAmount:   taxAmount,
		Notes:       r.FormValue("notes"),
	}

	lines := parseInvoiceLines(r)
	updated, err := h.Invoices.Update(r.Context(), invoiceModule.Actor{
		UserID: user.ID, CanManage: user.HasCapability(model.CapInvoicesManage),
	}, id, invoiceModule.Draft{
		ContactID: inv.ContactID, InvoiceDate: inv.InvoiceDate, DueDate: inv.DueDate,
		TaxAmount: inv.TaxAmount, Notes: inv.Notes, Lines: invoiceDraftLines(lines),
	})
	if err != nil {
		if errors.Is(err, invoiceModule.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		fd, formErr := h.newInvoiceFormData(r.Context())
		if formErr != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		fd.Invoice = inv
		fd.Lines = lines
		fd.Errors = invoiceFormErrors(err, lines)
		fd.IsEdit = true
		h.render(w, r, "templates/invoices/form.html", "Edit Invoice", fd, "templates/invoices/line_partial.html")
		return
	}

	h.setFlash(w, "Invoice updated successfully")
	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", updated.ID), http.StatusSeeOther)
}

func (h *Handler) SendInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user := auth.UserFromContext(r.Context())
	if _, err := h.Invoices.Send(r.Context(), invoiceModule.Actor{UserID: user.ID, CanManage: user.HasCapability(model.CapInvoicesManage)}, id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		h.setFlash(w, "Invoice sent — journal entry created")
	}

	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
}

// InvoiceWhatsApp opens WhatsApp to the invoice's contact with the parent
// portal link pre-filled, so staff can share it in one tap. Drafts aren't
// shareable (the portal hides them until sent), and a contact needs a phone
// number on file to be reachable this way.
func (h *Handler) InvoiceWhatsApp(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	inv, err := h.Invoices.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, invoiceModule.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}
	if inv.Status == model.StatusDraft {
		h.setFlash(w, "Kirim invoice ini dulu (Mark as Sent) sebelum membagikan link ke orang tua.")
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
		return
	}

	contact, err := model.GetContact(h.DB, inv.ContactID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if contact.Phone == "" {
		h.setFlash(w, "Nomor telepon kontak belum diisi.")
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
		return
	}

	code, err := model.GetOrCreatePortalCode(h.DB, contact.ID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	portalURL := h.publicOrigin(r) + "/p/" + code
	message := fmt.Sprintf(
		"Assalamualaikum Wr. Wb., kami dari Antar Jemput Latasya. Berikut link invoice Ananda %s (%s):\n%s\n\n"+
			"Link berisi daftar invoice dan akan terus aktif sesuai masa keikutsertaan antar jemput, "+
			"Terima kasih",
		contact.Name, inv.InvoiceNumber, portalURL)
	http.Redirect(w, r, buildWALink(contact.Phone, message), http.StatusFound)
}

func (h *Handler) InvoicePayment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user := auth.UserFromContext(r.Context())
	amount := parseIDR(r.FormValue("amount"))
	paymentDate := r.FormValue("payment_date")
	paymentAccountID, _ := strconv.Atoi(r.FormValue("payment_account"))

	if amount <= 0 || paymentDate == "" || paymentAccountID == 0 {
		h.setFlash(w, "Error: all payment fields are required")
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
		return
	}

	if _, err := h.Invoices.RecordPayment(r.Context(), invoiceModule.Actor{UserID: user.ID, CanManage: user.HasCapability(model.CapInvoicesManage)}, id,
		invoiceModule.Payment{Amount: amount, Date: paymentDate, AccountID: paymentAccountID}); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		h.setFlash(w, "Payment recorded successfully")
	}

	http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user := auth.UserFromContext(r.Context())
	if _, err := h.Invoices.Delete(r.Context(), invoiceModule.Actor{UserID: user.ID, CanManage: user.HasCapability(model.CapInvoicesManage)}, id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, h.BasePath+fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
		return
	}

	h.setFlash(w, "Invoice deleted")
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/invoices")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, h.BasePath+"/invoices", http.StatusSeeOther)
}

type invoicePrintData struct {
	Invoice *model.Invoice
	Company *model.CompanyProfile
}

func (h *Handler) PrintInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	document, err := h.Invoices.Document(r.Context(), id)
	if err != nil {
		if errors.Is(err, invoiceModule.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// Print uses a standalone template (no base layout)
	t, err := h.getTemplate("templates/invoices/print.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	pd := PageData{
		User:     auth.UserFromContext(r.Context()),
		Title:    "Invoice " + document.Invoice.InvoiceNumber,
		BasePath: h.BasePath,
		Data:     invoicePrintData{Invoice: document.Invoice, Company: document.Company},
	}
	t.ExecuteTemplate(w, "print.html", pd)
}

func (h *Handler) InvoicePDF(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	document, err := h.Invoices.Document(r.Context(), id)
	if err != nil {
		if errors.Is(err, invoiceModule.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	data, err := pdf.InvoicePDF(document.Invoice, document.Company)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", document.Invoice.InvoiceNumber+".pdf"))
	w.Write(data)
}

// HTMX partial for adding invoice lines
func (h *Handler) InvoiceLinePartial(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.Invoices.RevenueAccounts(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	t, err := h.getTemplate("templates/invoices/line_partial.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	t.ExecuteTemplate(w, "invoice-line", map[string]any{
		"Accounts": accounts,
	})
}

func parseInvoiceLines(r *http.Request) []model.InvoiceLine {
	descriptions := r.Form["line_description"]
	quantities := r.Form["line_quantity"]
	unitPrices := r.Form["line_unit_price"]
	accountIDs := r.Form["line_account_id"]

	var lines []model.InvoiceLine
	for i := range descriptions {
		desc := getIndex(descriptions, i)
		qty := parseQuantity(getIndex(quantities, i))
		if qty == 0 {
			qty = 100 // default 1.00
		}
		price := parseIDR(getIndex(unitPrices, i))
		accountID, _ := strconv.Atoi(getIndex(accountIDs, i))

		if desc == "" && price == 0 && accountID == 0 {
			continue
		}

		lines = append(lines, model.InvoiceLine{
			Description: desc,
			Quantity:    qty,
			UnitPrice:   price,
			AccountID:   accountID,
			Amount:      qty * price / 100,
		})
	}
	return lines
}

func invoiceDraftLines(lines []model.InvoiceLine) []invoiceModule.DraftLine {
	draftLines := make([]invoiceModule.DraftLine, len(lines))
	for i, line := range lines {
		draftLines[i] = invoiceModule.DraftLine{
			Description: line.Description,
			Quantity:    line.Quantity,
			UnitPrice:   line.UnitPrice,
			AccountID:   line.AccountID,
		}
	}
	return draftLines
}

func invoiceFormErrors(err error, lines []model.InvoiceLine) map[string]string {
	var validation *invoiceModule.ValidationError
	if !errors.As(err, &validation) {
		return map[string]string{"general": err.Error()}
	}

	fields := map[string]string{}
	for _, name := range []string{"contact_id", "invoice_date", "due_date", "tax_amount", "lines"} {
		if message := validation.Fields[name]; message != "" {
			fields[name] = message
		}
	}
	for i := range lines {
		prefix := fmt.Sprintf("lines[%d]", i)
		if message := validation.Fields[prefix+".description"]; message != "" {
			fields[fmt.Sprintf("line_%d_desc", i)] = message
		}
		if message := validation.Fields[prefix+".quantity"]; message != "" {
			fields[fmt.Sprintf("line_%d_quantity", i)] = message
		}
		if message := validation.Fields[prefix+".unit_price"]; message != "" {
			fields[fmt.Sprintf("line_%d_price", i)] = message
		}
		if message := validation.Fields[prefix+".account_id"]; message != "" {
			fields[fmt.Sprintf("line_%d_account", i)] = message
		}
	}
	return fields
}

func (h *Handler) newInvoiceFormData(ctx context.Context) (invoiceFormData, error) {
	options, err := h.Invoices.FormOptions(ctx)
	if err != nil {
		return invoiceFormData{}, err
	}
	fd := invoiceFormData{
		Errors: make(map[string]string),
	}
	fd.Contacts = options.Contacts
	fd.RevenueAccounts = options.RevenueAccounts
	fd.DefaultRevenueAccountID = options.DefaultRevenueAccountID
	fd.RecurringDescriptionTemplate = options.RecurringDescriptionTemplate
	return fd, nil
}
