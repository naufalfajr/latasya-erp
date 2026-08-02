package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/reporting"
)

func defaultDateRange() (string, string) {
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).Format("2006-01-02")
	to := now.Format("2006-01-02")
	return from, to
}

func getDateRange(r *http.Request) (string, string) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		from, to = defaultDateRange()
	}
	return from, to
}

func (h *Handler) TrialBalance(w http.ResponseWriter, r *http.Request) {
	from, to := getDateRange(r)

	rows, err := h.Reporting.TrialBalance(r.Context(), from, to)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var totalDebit, totalCredit int
	for _, row := range rows {
		totalDebit += row.TotalDebit
		totalCredit += row.TotalCredit
	}

	h.render(w, r, "templates/reports/trial_balance.html", "Trial Balance", map[string]any{
		"Rows":        rows,
		"TotalDebit":  totalDebit,
		"TotalCredit": totalCredit,
		"From":        from,
		"To":          to,
	})
}

func (h *Handler) ProfitLoss(w http.ResponseWriter, r *http.Request) {
	from, to := getDateRange(r)

	report, err := h.Reporting.ProfitLoss(r.Context(), from, to)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/reports/profit_loss.html", "Profit & Loss", map[string]any{
		"Report": report,
		"From":   from,
		"To":     to,
	})
}

func (h *Handler) BalanceSheet(w http.ResponseWriter, r *http.Request) {
	asOf := r.URL.Query().Get("date")
	if asOf == "" {
		asOf = time.Now().Format("2006-01-02")
	}

	report, err := h.Reporting.BalanceSheet(r.Context(), asOf)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/reports/balance_sheet.html", "Balance Sheet", map[string]any{
		"Report": report,
		"AsOf":   asOf,
	})
}

func (h *Handler) CashFlowReport(w http.ResponseWriter, r *http.Request) {
	from, to := getDateRange(r)

	report, err := h.Reporting.CashFlow(r.Context(), from, to)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, "templates/reports/cash_flow.html", "Cash Flow", map[string]any{
		"Report": report,
		"From":   from,
		"To":     to,
	})
}

func (h *Handler) GeneralLedger(w http.ResponseWriter, r *http.Request) {
	from, to := getDateRange(r)
	accountIDStr := r.URL.Query().Get("account")
	accountID, _ := strconv.Atoi(accountIDStr)

	active := true
	accountResult, _ := h.Accounts.List(r.Context(), account.Filter{IsActive: &active})

	var entries []reporting.GeneralLedgerEntry
	var selectedAccount *model.Account
	var totalDebit, totalCredit int
	if accountID > 0 {
		var err error
		entries, err = h.Reporting.GeneralLedger(r.Context(), accountID, from, to)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		selectedAccount, _ = h.Accounts.Get(r.Context(), accountID)
		for _, e := range entries {
			totalDebit += e.Debit
			totalCredit += e.Credit
		}
	}

	// Present the net in the account's natural sign so it matches how the P&L
	// and balance sheet show the same account: debit-normal accounts net as
	// debit-credit, credit-normal accounts (revenue/liability/equity) net as
	// credit-debit. Without this, a revenue account's footer would be negative
	// while its P&L line is positive, breaking the reconciliation this footer
	// exists for.
	net := totalDebit - totalCredit
	if selectedAccount != nil && selectedAccount.NormalBalance == "credit" {
		net = totalCredit - totalDebit
	}

	h.render(w, r, "templates/reports/general_ledger.html", "General Ledger", map[string]any{
		"Accounts":        accountResult.Accounts,
		"Entries":         entries,
		"SelectedAccount": selectedAccount,
		"AccountID":       accountID,
		"From":            from,
		"To":              to,
		"TotalDebit":      totalDebit,
		"TotalCredit":     totalCredit,
		"Net":             net,
	})
}

func (h *Handler) RegisterReportingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.Dashboard)
	mux.HandleFunc("GET /reports/trial-balance", h.TrialBalance)
	mux.HandleFunc("GET /reports/profit-loss", h.ProfitLoss)
	mux.HandleFunc("GET /reports/balance-sheet", h.BalanceSheet)
	mux.HandleFunc("GET /reports/cash-flow", h.CashFlowReport)
	mux.HandleFunc("GET /reports/general-ledger", h.GeneralLedger)
}
