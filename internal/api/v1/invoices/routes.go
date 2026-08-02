package invoices

import "net/http"

// Middleware wraps invoice actions that require idempotency handling.
type Middleware func(http.Handler) http.Handler

// RegisterRoutes installs all v1 invoice API endpoints.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, idempotency Middleware) {
	mux.HandleFunc("GET /api/v1/invoices", h.List)
	mux.HandleFunc("GET /api/v1/invoices/{id}", h.Get)
	mux.HandleFunc("GET /api/v1/invoices/{id}/pdf", h.PDF)
	mux.Handle("POST /api/v1/invoices", idempotency(http.HandlerFunc(h.Create)))
	mux.Handle("PUT /api/v1/invoices/{id}", idempotency(http.HandlerFunc(h.Update)))
	mux.HandleFunc("DELETE /api/v1/invoices/{id}", h.Delete)
	mux.Handle("POST /api/v1/invoices/{id}/send", idempotency(http.HandlerFunc(h.Send)))
	mux.Handle("POST /api/v1/invoices/{id}/payment", idempotency(http.HandlerFunc(h.Payment)))
	mux.Handle("POST /api/v1/invoices/generate-recurring", idempotency(http.HandlerFunc(h.GenerateRecurring)))
	mux.HandleFunc("POST /api/v1/invoices/bulk-delete", h.BulkDelete)
	mux.HandleFunc("POST /api/v1/invoices/bulk-send", h.BulkSend)
}
