package reports

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct {
	DB *sql.DB
}

func idr(n int) string {
	return fmt.Sprintf("%d", n)
}

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

type trialBalanceRowResp struct {
	AccountID     int    `json:"account_id"`
	AccountCode   string `json:"account_code"`
	AccountName   string `json:"account_name"`
	AccountType   string `json:"account_type"`
	NormalBalance string `json:"normal_balance"`
	TotalDebit    string `json:"total_debit"`
	TotalCredit   string `json:"total_credit"`
	Balance       string `json:"balance"`
}

type trialBalanceResp struct {
	Rows        []trialBalanceRowResp `json:"rows"`
	TotalDebit  string                `json:"total_debit"`
	TotalCredit string                `json:"total_credit"`
	From        string                `json:"from"`
	To          string                `json:"to"`
}

func (h *Handler) TrialBalance(w http.ResponseWriter, r *http.Request) {
	from, to := getDateRange(r)

	rows, err := model.TrialBalance(h.DB, from, to)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to generate trial balance", nil)
		return
	}

	var totalDebit, totalCredit int
	resp := trialBalanceResp{From: from, To: to, Rows: make([]trialBalanceRowResp, 0, len(rows))}
	for _, row := range rows {
		totalDebit += row.TotalDebit
		totalCredit += row.TotalCredit
		resp.Rows = append(resp.Rows, trialBalanceRowResp{
			AccountID:     row.AccountID,
			AccountCode:   row.AccountCode,
			AccountName:   row.AccountName,
			AccountType:   row.AccountType,
			NormalBalance: row.NormalBalance,
			TotalDebit:    idr(row.TotalDebit),
			TotalCredit:   idr(row.TotalCredit),
			Balance:       idr(row.Balance),
		})
	}
	resp.TotalDebit = idr(totalDebit)
	resp.TotalCredit = idr(totalCredit)

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": resp})
}

type profitLossRowResp struct {
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	AccountType string `json:"account_type"`
	Amount      string `json:"amount"`
}

type profitLossResp struct {
	Revenue      []profitLossRowResp `json:"revenue"`
	Expenses     []profitLossRowResp `json:"expenses"`
	TotalRevenue string              `json:"total_revenue"`
	TotalExpense string              `json:"total_expense"`
	NetIncome    string              `json:"net_income"`
	From         string              `json:"from"`
	To           string              `json:"to"`
}

func (h *Handler) ProfitLoss(w http.ResponseWriter, r *http.Request) {
	from, to := getDateRange(r)

	report, err := model.ProfitLoss(h.DB, from, to)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to generate profit & loss", nil)
		return
	}

	resp := profitLossResp{
		From:         from,
		To:           to,
		TotalRevenue: idr(report.TotalRevenue),
		TotalExpense: idr(report.TotalExpense),
		NetIncome:    idr(report.NetIncome),
		Revenue:      make([]profitLossRowResp, 0, len(report.Revenue)),
		Expenses:     make([]profitLossRowResp, 0, len(report.Expenses)),
	}
	for _, row := range report.Revenue {
		resp.Revenue = append(resp.Revenue, profitLossRowResp{
			AccountCode: row.AccountCode,
			AccountName: row.AccountName,
			AccountType: row.AccountType,
			Amount:      idr(row.Amount),
		})
	}
	for _, row := range report.Expenses {
		resp.Expenses = append(resp.Expenses, profitLossRowResp{
			AccountCode: row.AccountCode,
			AccountName: row.AccountName,
			AccountType: row.AccountType,
			Amount:      idr(row.Amount),
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": resp})
}

type balanceSheetRowResp struct {
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	Balance     string `json:"balance"`
}

type balanceSheetSectionResp struct {
	Accounts []balanceSheetRowResp `json:"accounts"`
	Total    string                `json:"total"`
}

type balanceSheetResp struct {
	Assets           balanceSheetSectionResp `json:"assets"`
	Liabilities      balanceSheetSectionResp `json:"liabilities"`
	Equity           balanceSheetSectionResp `json:"equity"`
	RetainedEarnings string                  `json:"retained_earnings"`
	TotalLiabEquity  string                  `json:"total_liab_equity"`
	AsOf             string                  `json:"as_of"`
}

func toSectionResp(s model.BalanceSheetSection) balanceSheetSectionResp {
	rows := make([]balanceSheetRowResp, 0, len(s.Accounts))
	for _, a := range s.Accounts {
		rows = append(rows, balanceSheetRowResp{
			AccountCode: a.AccountCode,
			AccountName: a.AccountName,
			Balance:     idr(a.Balance),
		})
	}
	return balanceSheetSectionResp{Accounts: rows, Total: idr(s.Total)}
}

func (h *Handler) BalanceSheet(w http.ResponseWriter, r *http.Request) {
	asOf := r.URL.Query().Get("date")
	if asOf == "" {
		asOf = time.Now().Format("2006-01-02")
	}

	report, err := model.BalanceSheet(h.DB, asOf)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to generate balance sheet", nil)
		return
	}

	resp := balanceSheetResp{
		Assets:           toSectionResp(report.Assets),
		Liabilities:      toSectionResp(report.Liabilities),
		Equity:           toSectionResp(report.Equity),
		RetainedEarnings: idr(report.RetainedEarnings),
		TotalLiabEquity:  idr(report.TotalLiabEquity),
		AsOf:             asOf,
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": resp})
}

type cashFlowRowResp struct {
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	Amount      string `json:"amount"`
}

type cashFlowResp struct {
	Operating      []cashFlowRowResp `json:"operating"`
	TotalOperating string            `json:"total_operating"`
	NetCashChange  string            `json:"net_cash_change"`
	OpeningCash    string            `json:"opening_cash"`
	ClosingCash    string            `json:"closing_cash"`
	From           string            `json:"from"`
	To             string            `json:"to"`
}

func (h *Handler) CashFlow(w http.ResponseWriter, r *http.Request) {
	from, to := getDateRange(r)

	report, err := model.CashFlow(h.DB, from, to)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to generate cash flow", nil)
		return
	}

	resp := cashFlowResp{
		From:           from,
		To:             to,
		TotalOperating: idr(report.TotalOperating),
		NetCashChange:  idr(report.NetCashChange),
		OpeningCash:    idr(report.OpeningCash),
		ClosingCash:    idr(report.ClosingCash),
		Operating:      make([]cashFlowRowResp, 0, len(report.Operating)),
	}
	for _, row := range report.Operating {
		resp.Operating = append(resp.Operating, cashFlowRowResp{
			AccountCode: row.AccountCode,
			AccountName: row.AccountName,
			Amount:      idr(row.Amount),
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": resp})
}

type generalLedgerEntryResp struct {
	EntryDate   string `json:"entry_date"`
	Reference   string `json:"reference"`
	Description string `json:"description"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	Balance     string `json:"balance"`
}

type generalLedgerResp struct {
	AccountID   int                      `json:"account_id"`
	AccountCode string                   `json:"account_code"`
	AccountName string                   `json:"account_name"`
	Entries     []generalLedgerEntryResp `json:"entries"`
	From        string                   `json:"from"`
	To          string                   `json:"to"`
}

func (h *Handler) GeneralLedger(w http.ResponseWriter, r *http.Request) {
	from, to := getDateRange(r)
	accountIDStr := r.URL.Query().Get("account")
	accountID, err := strconv.Atoi(accountIDStr)
	if err != nil || accountID <= 0 {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "account query parameter required (integer account id)", nil)
		return
	}

	account, err := model.GetAccount(h.DB, accountID)
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "account not found", nil)
		return
	}

	entries, err := model.GeneralLedger(h.DB, accountID, from, to)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to generate general ledger", nil)
		return
	}

	resp := generalLedgerResp{
		AccountID:   accountID,
		AccountCode: account.Code,
		AccountName: account.Name,
		From:        from,
		To:          to,
		Entries:     make([]generalLedgerEntryResp, 0, len(entries)),
	}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, generalLedgerEntryResp{
			EntryDate:   e.EntryDate,
			Reference:   e.Reference,
			Description: e.Description,
			Debit:       idr(e.Debit),
			Credit:      idr(e.Credit),
			Balance:     idr(e.Balance),
		})
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": resp})
}
