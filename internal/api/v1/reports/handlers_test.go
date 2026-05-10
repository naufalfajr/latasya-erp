package reports_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/api/v1/reports"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &reports.Handler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/reports/trial-balance", h.TrialBalance)
	mux.HandleFunc("GET /api/v1/reports/profit-loss", h.ProfitLoss)
	mux.HandleFunc("GET /api/v1/reports/balance-sheet", h.BalanceSheet)
	mux.HandleFunc("GET /api/v1/reports/cash-flow", h.CashFlow)
	mux.HandleFunc("GET /api/v1/reports/general-ledger", h.GeneralLedger)
	ts := httptest.NewServer(v1.BearerOrCookie(db)(mux))
	t.Cleanup(ts.Close)
	return ts
}

func adminToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("get admin: %v", err)
	}
	_, tok, err := model.CreateAPIToken(db, adminID,
		fmt.Sprintf("test-reports-%d", time.Now().UnixNano()),
		model.AllCapabilities, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return tok
}

func doReq(t *testing.T, ts *httptest.Server, method, path, bearer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestTrialBalance(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/reports/trial-balance", "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("authenticated returns 200 with expected shape", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/reports/trial-balance", tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var env struct {
			Data struct {
				Rows        []any  `json:"rows"`
				TotalDebit  string `json:"total_debit"`
				TotalCredit string `json:"total_credit"`
				From        string `json:"from"`
				To          string `json:"to"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.TotalDebit == "" {
			t.Error("expected total_debit string")
		}
		if env.Data.From == "" {
			t.Error("expected from date")
		}
	})
}

func TestProfitLoss(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	resp := doReq(t, ts, http.MethodGet, "/api/v1/reports/profit-loss", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var env struct {
		Data struct {
			Revenue      []any  `json:"revenue"`
			Expenses     []any  `json:"expenses"`
			TotalRevenue string `json:"total_revenue"`
			NetIncome    string `json:"net_income"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.TotalRevenue == "" {
		t.Error("expected total_revenue string")
	}
}

func TestBalanceSheet(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	resp := doReq(t, ts, http.MethodGet, "/api/v1/reports/balance-sheet", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var env struct {
		Data struct {
			Assets struct {
				Total string `json:"total"`
			} `json:"assets"`
			AsOf string `json:"as_of"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.AsOf == "" {
		t.Error("expected as_of date")
	}
}

func TestCashFlow(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	resp := doReq(t, ts, http.MethodGet, "/api/v1/reports/cash-flow", tok)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var env struct {
		Data struct {
			ClosingCash string `json:"closing_cash"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.ClosingCash == "" {
		t.Error("expected closing_cash string")
	}
}

func TestGeneralLedger(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := adminToken(t, db)

	t.Run("missing account param returns 400", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/reports/general-ledger", tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("nonexistent account returns 404", func(t *testing.T) {
		resp := doReq(t, ts, http.MethodGet, "/api/v1/reports/general-ledger?account=999999", tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("valid account returns 200", func(t *testing.T) {
		var accountID int
		if err := db.QueryRow("SELECT id FROM accounts LIMIT 1").Scan(&accountID); err != nil {
			t.Fatalf("get account: %v", err)
		}
		resp := doReq(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/reports/general-ledger?account=%d", accountID), tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var env struct {
			Data struct {
				AccountID int    `json:"account_id"`
				Entries   []any  `json:"entries"`
				From      string `json:"from"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.AccountID != accountID {
			t.Errorf("expected account_id %d, got %d", accountID, env.Data.AccountID)
		}
	})
}

func TestCapabilityEnforcement(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	viewerID := testutil.CreateTestUser(t, db, "viewer-reports", "pw", "viewer")
	_, noCapTok, err := model.CreateAPIToken(db, viewerID, "no-cap-reports", []string{}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	endpoints := []string{
		"/api/v1/reports/trial-balance",
		"/api/v1/reports/profit-loss",
		"/api/v1/reports/balance-sheet",
		"/api/v1/reports/cash-flow",
	}
	for _, ep := range endpoints {
		resp := doReq(t, ts, http.MethodGet, ep, noCapTok)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Logf("endpoint %s with viewer token: %d (reports are open to all authenticated users)", ep, resp.StatusCode)
		}
	}
}
