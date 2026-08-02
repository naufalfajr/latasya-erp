package journals

import "net/http"

type Middleware func(http.Handler) http.Handler

func (h *Handler) RegisterRoutes(mux *http.ServeMux, idempotency Middleware) {
	mux.HandleFunc("GET /api/v1/journals", h.List)
	mux.HandleFunc("GET /api/v1/journals/{id}", h.Get)
	mux.Handle("POST /api/v1/journals", idempotency(http.HandlerFunc(h.Create)))
	mux.Handle("PUT /api/v1/journals/{id}", idempotency(http.HandlerFunc(h.Update)))
	mux.HandleFunc("DELETE /api/v1/journals/{id}", h.Delete)
}
