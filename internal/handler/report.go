package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
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

	rows, err := model.TrialBalance(h.DB, from, to)
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

	report, err := model.ProfitLoss(h.DB, from, to)
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

	report, err := model.BalanceSheet(h.DB, asOf)
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

	report, err := model.CashFlow(h.DB, from, to)
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
	accounts, _ := model.ListAccounts(h.DB, model.AccountFilter{IsActive: &active})

	var entries []model.GeneralLedgerEntry
	var selectedAccount *model.Account
	if accountID > 0 {
		var err error
		entries, err = model.GeneralLedger(h.DB, accountID, from, to)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		selectedAccount, _ = model.GetAccount(h.DB, accountID)
	}

	h.render(w, r, "templates/reports/general_ledger.html", "General Ledger", map[string]any{
		"Accounts":        accounts,
		"Entries":         entries,
		"SelectedAccount": selectedAccount,
		"AccountID":       accountID,
		"From":            from,
		"To":              to,
	})
}
