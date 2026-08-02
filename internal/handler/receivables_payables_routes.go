package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// RegisterReceivablesPayablesRoutes installs bill and credit-note HTML/HTMX endpoints.
func (h *Handler) RegisterReceivablesPayablesRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /credit-notes", h.ListCreditNotes)
	mux.HandleFunc("GET /credit-notes/new", h.NewCreditNote)
	mux.HandleFunc("POST /credit-notes", auth.CapabilityOnly(model.CapInvoicesManage, h.CreateCreditNote))
	mux.HandleFunc("GET /credit-notes/{id}", h.ViewCreditNote)
	mux.HandleFunc("GET /credit-notes/{id}/edit", h.EditCreditNote)
	mux.HandleFunc("POST /credit-notes/{id}", auth.CapabilityOnly(model.CapInvoicesManage, h.UpdateCreditNote))
	mux.HandleFunc("DELETE /credit-notes/{id}", auth.CapabilityOnly(model.CapInvoicesManage, h.DeleteCreditNote))
	mux.HandleFunc("POST /credit-notes/{id}/issue", auth.CapabilityOnly(model.CapInvoicesManage, h.IssueCreditNote))
	mux.HandleFunc("POST /credit-notes/{id}/void", auth.CapabilityOnly(model.CapInvoicesManage, h.VoidCreditNote))
	mux.HandleFunc("GET /htmx/credit-note-line", h.CreditNoteLinePartial)

	mux.HandleFunc("GET /bills", h.ListBills)
	mux.HandleFunc("GET /bills/new", h.NewBill)
	mux.HandleFunc("POST /bills", auth.CapabilityOnly(model.CapBillsManage, h.CreateBill))
	mux.HandleFunc("GET /bills/{id}", h.ViewBill)
	mux.HandleFunc("GET /bills/{id}/edit", h.EditBill)
	mux.HandleFunc("POST /bills/{id}", auth.CapabilityOnly(model.CapBillsManage, h.UpdateBill))
	mux.HandleFunc("DELETE /bills/{id}", auth.CapabilityOnly(model.CapBillsManage, h.DeleteBill))
	mux.HandleFunc("POST /bills/{id}/receive", auth.CapabilityOnly(model.CapBillsManage, h.ReceiveBill))
	mux.HandleFunc("POST /bills/{id}/payment", auth.CapabilityOnly(model.CapBillsManage, h.BillPayment))
	mux.HandleFunc("GET /htmx/bill-line", h.BillLinePartial)
}
