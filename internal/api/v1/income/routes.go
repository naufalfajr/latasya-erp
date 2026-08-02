package income

import "net/http"

type Middleware func(http.Handler) http.Handler

func (h *Handler) RegisterRoutes(mux *http.ServeMux, idempotency Middleware) {
	mux.HandleFunc("GET /api/v1/income", h.List)
	mux.HandleFunc("GET /api/v1/income/{id}", h.Get)
	mux.Handle("POST /api/v1/income", idempotency(http.HandlerFunc(h.Create)))
	mux.Handle("PUT /api/v1/income/{id}", idempotency(http.HandlerFunc(h.Update)))
	mux.HandleFunc("DELETE /api/v1/income/{id}", h.Delete)
}
