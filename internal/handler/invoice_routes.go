package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// RegisterInvoiceRoutes installs the invoice HTML and HTMX endpoints.
func (h *Handler) RegisterInvoiceRoutes(mux *http.ServeMux) {
	manage := func(next http.HandlerFunc) http.HandlerFunc {
		return auth.CapabilityOnly(model.CapInvoicesManage, next)
	}

	mux.HandleFunc("GET /invoices", h.ListInvoices)
	mux.HandleFunc("GET /invoices/new", h.NewInvoice)
	mux.HandleFunc("POST /invoices", manage(h.CreateInvoice))
	mux.HandleFunc("POST /invoices/generate-recurring", manage(h.GenerateRecurringInvoices))
	mux.HandleFunc("POST /invoices/bulk-delete", manage(h.BulkDeleteInvoices))
	mux.HandleFunc("POST /invoices/bulk-send", manage(h.BulkSendInvoices))
	mux.HandleFunc("GET /invoices/{id}", h.ViewInvoice)
	mux.HandleFunc("GET /invoices/{id}/edit", h.EditInvoice)
	mux.HandleFunc("POST /invoices/{id}", manage(h.UpdateInvoice))
	mux.HandleFunc("DELETE /invoices/{id}", manage(h.DeleteInvoice))
	mux.HandleFunc("POST /invoices/{id}/send", manage(h.SendInvoice))
	mux.HandleFunc("POST /invoices/{id}/payment", manage(h.InvoicePayment))
	mux.HandleFunc("GET /invoices/{id}/print", h.PrintInvoice)
	mux.HandleFunc("GET /invoices/{id}/pdf", h.InvoicePDF)
	mux.HandleFunc("GET /invoices/{id}/whatsapp", manage(h.InvoiceWhatsApp))
	mux.HandleFunc("GET /htmx/invoice-line", h.InvoiceLinePartial)
}
