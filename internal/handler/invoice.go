package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type invoiceFormData struct {
	Invoice         *model.Invoice
	Lines           []model.InvoiceLine
	Contacts        []model.Contact
	RevenueAccounts []model.Account
	AssetAccounts   []model.Account
	Errors          map[string]string
	IsEdit          bool
}

func (h *Handler) ListInvoices(w http.ResponseWriter, r *http.Request) {
	f := model.InvoiceFilter{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
	}

	invoices, err := model.ListInvoices(h.DB, f)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/invoices/index.html", "Invoices", map[string]any{
		"Invoices": invoices,
		"Filter":   f.Status,
		"Search":   f.Search,
	})
}

func (h *Handler) NewInvoice(w http.ResponseWriter, r *http.Request) {
	fd := h.newInvoiceFormData()
	fd.Invoice = &model.Invoice{}
	fd.Lines = []model.InvoiceLine{{Quantity: 100}, {Quantity: 100}} // 2 empty lines, qty=1.00
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
		CreatedBy:   user.ID,
	}

	lines := parseInvoiceLines(r)
	errors := validateInvoice(inv, lines)

	if len(errors) > 0 {
		fd := h.newInvoiceFormData()
		fd.Invoice = inv
		fd.Lines = lines
		fd.Errors = errors
		h.render(w, r, "templates/invoices/form.html", "New Invoice", fd, "templates/invoices/line_partial.html")
		return
	}

	invID, err := model.CreateInvoice(h.DB, inv, lines)
	if err != nil {
		fd := h.newInvoiceFormData()
		fd.Invoice = inv
		fd.Lines = lines
		fd.Errors = map[string]string{"general": err.Error()}
		h.render(w, r, "templates/invoices/form.html", "New Invoice", fd, "templates/invoices/line_partial.html")
		return
	}

	created, _ := model.GetInvoice(h.DB, invID)
	invoiceNumber := ""
	total := 0
	if created != nil {
		invoiceNumber = created.InvoiceNumber
		total = created.Total
	}
	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "invoice.create",
		TargetType:  "invoice",
		TargetID:    int64(invID),
		TargetLabel: invoiceNumber,
		Metadata: map[string]any{
			"after": map[string]any{
				"contact_id":   inv.ContactID,
				"invoice_date": inv.InvoiceDate,
				"due_date":     inv.DueDate,
				"tax_amount":   inv.TaxAmount,
				"total":        total,
				"line_count":   len(lines),
			},
		},
	})

	h.setFlash(w, "Invoice created successfully")
	http.Redirect(w, r, fmt.Sprintf("/invoices/%d", invID), http.StatusSeeOther)
}

func (h *Handler) ViewInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	inv, err := model.GetInvoice(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	active := true
	assetAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "asset", IsActive: &active})

	h.render(w, r, "templates/invoices/view.html", "Invoice "+inv.InvoiceNumber, map[string]any{
		"Invoice":       inv,
		"AssetAccounts": assetAccounts,
	})
}

func (h *Handler) EditInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	inv, err := model.GetInvoice(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if inv.Status != "draft" {
		h.setFlash(w, "Can only edit draft invoices")
		http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
		return
	}

	fd := h.newInvoiceFormData()
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

	// Snapshot the pre-update state so audit can record a before/after diff.
	oldInv, err := model.GetInvoice(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	r.ParseForm()
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
	errors := validateInvoice(inv, lines)

	if len(errors) > 0 {
		fd := h.newInvoiceFormData()
		fd.Invoice = inv
		fd.Lines = lines
		fd.Errors = errors
		fd.IsEdit = true
		h.render(w, r, "templates/invoices/form.html", "Edit Invoice", fd, "templates/invoices/line_partial.html")
		return
	}

	if err := model.UpdateInvoice(h.DB, inv, lines); err != nil {
		fd := h.newInvoiceFormData()
		fd.Invoice = inv
		fd.Lines = lines
		fd.Errors = map[string]string{"general": err.Error()}
		fd.IsEdit = true
		h.render(w, r, "templates/invoices/form.html", "Edit Invoice", fd, "templates/invoices/line_partial.html")
		return
	}

	// Re-fetch to capture derived totals after the line rewrite.
	newInv, _ := model.GetInvoice(h.DB, id)
	oldFields := map[string]any{
		"contact_id":   oldInv.ContactID,
		"invoice_date": oldInv.InvoiceDate,
		"due_date":     oldInv.DueDate,
		"tax_amount":   oldInv.TaxAmount,
		"notes":        oldInv.Notes,
		"total":        oldInv.Total,
	}
	newFields := map[string]any{
		"contact_id":   inv.ContactID,
		"invoice_date": inv.InvoiceDate,
		"due_date":     inv.DueDate,
		"tax_amount":   inv.TaxAmount,
		"notes":        inv.Notes,
	}
	if newInv != nil {
		newFields["total"] = newInv.Total
	}
	metadata := audit.Diff(oldFields, newFields,
		[]string{"contact_id", "invoice_date", "due_date", "tax_amount", "notes", "total"})
	if metadata != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "invoice.update",
			TargetType:  "invoice",
			TargetID:    int64(id),
			TargetLabel: oldInv.InvoiceNumber,
			Metadata:    metadata,
		})
	}

	h.setFlash(w, "Invoice updated successfully")
	http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
}

func (h *Handler) SendInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	user := auth.UserFromContext(r.Context())
	if err := model.SendInvoice(h.DB, id, user.ID); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		if inv, err := model.GetInvoice(h.DB, id); err == nil {
			audit.Log(r.Context(), h.DB, audit.Event{
				Action:      "invoice.send",
				TargetType:  "invoice",
				TargetID:    int64(id),
				TargetLabel: inv.InvoiceNumber,
				Metadata: map[string]any{
					"after":      map[string]any{"status": inv.Status},
					"journal_id": inv.JournalID,
				},
			})
		}
		h.setFlash(w, "Invoice sent — journal entry created")
	}

	http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
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
		http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
		return
	}

	if err := model.RecordInvoicePayment(h.DB, id, amount, paymentDate, paymentAccountID, user.ID); err != nil {
		h.setFlash(w, "Error: "+err.Error())
	} else {
		inv, _ := model.GetInvoice(h.DB, id)
		invoiceNumber := ""
		status := ""
		if inv != nil {
			invoiceNumber = inv.InvoiceNumber
			status = inv.Status
		}
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "invoice.payment",
			TargetType:  "invoice",
			TargetID:    int64(id),
			TargetLabel: invoiceNumber,
			Metadata: map[string]any{
				"amount":             amount,
				"payment_date":       paymentDate,
				"payment_account_id": paymentAccountID,
				"status_after":       status,
			},
		})
		h.setFlash(w, "Payment recorded successfully")
	}

	http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
}

func (h *Handler) DeleteInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Snapshot pre-delete so the audit row carries enough context to
	// understand what disappeared.
	existing, _ := model.GetInvoice(h.DB, id)

	if err := model.DeleteInvoice(h.DB, id); err != nil {
		h.setFlash(w, "Error: "+err.Error())
		http.Redirect(w, r, fmt.Sprintf("/invoices/%d", id), http.StatusSeeOther)
		return
	}

	if existing != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "invoice.delete",
			TargetType:  "invoice",
			TargetID:    int64(id),
			TargetLabel: existing.InvoiceNumber,
			Metadata: map[string]any{
				"before": map[string]any{
					"contact_id":   existing.ContactID,
					"invoice_date": existing.InvoiceDate,
					"status":       existing.Status,
					"total":        existing.Total,
				},
			},
		})
	}

	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusOK)
		return
	}

	h.setFlash(w, "Invoice deleted")
	http.Redirect(w, r, "/invoices", http.StatusSeeOther)
}

func (h *Handler) PrintInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	inv, err := model.GetInvoice(h.DB, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Print uses a standalone template (no base layout)
	t, err := h.getTemplate("templates/invoices/print.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	pd := PageData{
		User:  auth.UserFromContext(r.Context()),
		Title: "Invoice " + inv.InvoiceNumber,
		Data:  inv,
	}
	t.ExecuteTemplate(w, "templates/invoices/print.html", pd)
}

// HTMX partial for adding invoice lines
func (h *Handler) InvoiceLinePartial(w http.ResponseWriter, r *http.Request) {
	active := true
	accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "revenue", IsActive: &active})

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

func validateInvoice(inv *model.Invoice, lines []model.InvoiceLine) map[string]string {
	errors := make(map[string]string)
	if inv.ContactID == 0 {
		errors["contact_id"] = "Customer is required"
	}
	if inv.InvoiceDate == "" {
		errors["invoice_date"] = "Invoice date is required"
	}
	if inv.DueDate == "" {
		errors["due_date"] = "Due date is required"
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

func (h *Handler) newInvoiceFormData() invoiceFormData {
	active := true
	contacts, _ := model.ListContacts(h.DB, model.ContactFilter{Type: "customer", IsActive: &active})
	revenueAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "revenue", IsActive: &active})
	assetAccounts, _ := model.ListAccounts(h.DB, model.AccountFilter{Type: "asset", IsActive: &active})

	return invoiceFormData{
		Contacts:        contacts,
		RevenueAccounts: revenueAccounts,
		AssetAccounts:   assetAccounts,
		Errors:          make(map[string]string),
	}
}
