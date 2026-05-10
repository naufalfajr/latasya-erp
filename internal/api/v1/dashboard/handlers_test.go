package dashboard_test

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
	"github.com/naufal/latasya-erp/internal/api/v1/dashboard"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &dashboard.Handler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/dashboard", h.Get)
	ts := httptest.NewServer(v1.BearerOrCookie(db)(mux))
	t.Cleanup(ts.Close)
	return ts
}

func anyAuthToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("get admin: %v", err)
	}
	_, tok, err := model.CreateAPIToken(db, adminID,
		fmt.Sprintf("test-dashboard-%d", time.Now().UnixNano()),
		model.AllCapabilities, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return tok
}

func doReq(t *testing.T, ts *httptest.Server, bearer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/dashboard", bytes.NewReader(nil))
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

func TestGetDashboard(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := anyAuthToken(t, db)

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		resp := doReq(t, ts, "")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("authenticated returns 200 with currency strings", func(t *testing.T) {
		resp := doReq(t, ts, tok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var env struct {
			Data struct {
				CashBalance         string `json:"cash_balance"`
				MonthlyRevenue      string `json:"monthly_revenue"`
				MonthlyExpenses     string `json:"monthly_expenses"`
				OutstandingInvoices string `json:"outstanding_invoices"`
				OutstandingBills    string `json:"outstanding_bills"`
				RecentTransactions  []any  `json:"recent_transactions"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.CashBalance == "" {
			t.Error("expected cash_balance string")
		}
		if env.Data.MonthlyRevenue == "" {
			t.Error("expected monthly_revenue string")
		}
		if env.Data.RecentTransactions == nil {
			t.Error("expected non-nil recent_transactions")
		}
	})

	t.Run("viewer with no caps can access dashboard", func(t *testing.T) {
		viewerID := testutil.CreateTestUser(t, db, "viewer-dash", "pw", "viewer")
		_, viewerTok, err := model.CreateAPIToken(db, viewerID, "viewer-dash-tok", []string{}, nil)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		resp := doReq(t, ts, viewerTok)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for any authenticated user, got %d", resp.StatusCode)
		}
	})
}

func TestDashboardCurrencyStrings(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	tok := anyAuthToken(t, db)

	resp := doReq(t, ts, tok)
	defer resp.Body.Close()

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, ok := raw["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data object")
	}
	for _, field := range []string{"cash_balance", "monthly_revenue", "monthly_expenses", "outstanding_invoices", "outstanding_bills"} {
		val, exists := data[field]
		if !exists {
			t.Errorf("missing field %s", field)
			continue
		}
		if _, ok := val.(string); !ok {
			t.Errorf("field %s should be string, got %T", field, val)
		}
	}
}
