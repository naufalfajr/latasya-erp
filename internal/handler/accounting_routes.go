package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// RegisterAccountingRoutes installs manual journal, income, expense, and HTMX
// endpoints so production and integration tests share one route definition.
func (h *Handler) RegisterAccountingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /journals", h.ListJournals)
	mux.HandleFunc("GET /journals/new", h.NewJournal)
	mux.HandleFunc("POST /journals", auth.CapabilityOnly(model.CapJournalsManage, h.CreateJournal))
	mux.HandleFunc("GET /journals/{id}", h.ViewJournal)
	mux.HandleFunc("GET /journals/{id}/edit", h.EditJournal)
	mux.HandleFunc("POST /journals/{id}", auth.CapabilityOnly(model.CapJournalsManage, h.UpdateJournal))
	mux.HandleFunc("DELETE /journals/{id}", auth.CapabilityOnly(model.CapJournalsManage, h.DeleteJournal))

	mux.HandleFunc("GET /income", h.ListIncome)
	mux.HandleFunc("GET /income/new", h.NewIncome)
	mux.HandleFunc("POST /income", auth.CapabilityOnly(model.CapIncomeManage, h.CreateIncome))
	mux.HandleFunc("GET /income/{id}/edit", h.EditIncome)
	mux.HandleFunc("POST /income/{id}", auth.CapabilityOnly(model.CapIncomeManage, h.UpdateIncome))
	mux.HandleFunc("DELETE /income/{id}", auth.CapabilityOnly(model.CapIncomeManage, h.DeleteIncome))

	mux.HandleFunc("GET /expenses", h.ListExpenses)
	mux.HandleFunc("GET /expenses/new", h.NewExpense)
	mux.HandleFunc("POST /expenses", auth.CapabilityOnly(model.CapExpensesManage, h.CreateExpense))
	mux.HandleFunc("GET /expenses/{id}/edit", h.EditExpense)
	mux.HandleFunc("POST /expenses/{id}", auth.CapabilityOnly(model.CapExpensesManage, h.UpdateExpense))
	mux.HandleFunc("DELETE /expenses/{id}", auth.CapabilityOnly(model.CapExpensesManage, h.DeleteExpense))

	mux.HandleFunc("GET /htmx/journal-line", h.JournalLinePartial)
}
