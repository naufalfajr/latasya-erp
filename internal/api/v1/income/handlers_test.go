package income_test

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
	v1income "github.com/naufal/latasya-erp/internal/api/v1/income"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func setupServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)

	h := &v1income.Handler{DB: db}
	idem := v1.Idempotency(db)

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/v1/income", h.List)
	apiMux.HandleFunc("GET /api/v1/income/{id}", h.Get)
	apiMux.Handle("POST /api/v1/income", idem(http.HandlerFunc(h.Create)))
	apiMux.Handle("PUT /api/v1/income/{id}", idem(http.HandlerFunc(h.Update)))
	apiMux.HandleFunc("DELETE /api/v1/income/{id}", h.Delete)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", v1.BearerOrCookie(db)(apiMux))

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, db
}

func adminToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("get admin: %v", err)
	}
	_, plaintext, err := model.CreateAPIToken(db, adminID,
		fmt.Sprintf("test-income-%d", time.Now().UnixNano()),
		[]string{model.CapIncomeManage}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return plaintext
}

func noScopeToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	userID := testutil.CreateTestUser(t, db, fmt.Sprintf("noscope-%d", time.Now().UnixNano()), "pw", model.RoleViewer)
	_, plaintext, err := model.CreateAPIToken(db, userID, "noscope", []string{}, nil)
	if err != nil {
		t.Fatalf("create no-scope token: %v", err)
	}
	return plaintext
}

func revenueAccountID(t *testing.T, db *sql.DB) int {
	t.Helper()
	var id int
	err := db.QueryRow("SELECT id FROM accounts WHERE account_type = 'revenue' LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatalf("find revenue account: %v", err)
	}
	return id
}

func assetAccountID(t *testing.T, db *sql.DB) int {
	t.Helper()
	var id int
	err := db.QueryRow("SELECT id FROM accounts WHERE account_type = 'asset' LIMIT 1").Scan(&id)
	if err != nil {
		t.Fatalf("find asset account: %v", err)
	}
	return id
}

func doRequest(t *testing.T, ts *httptest.Server, method, path, token string, body any) *http.Response {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req, err := http.NewRequest(method, ts.URL+path, buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func doRequestWithKey(t *testing.T, ts *httptest.Server, method, path, token, idemKey string, body any) *http.Response {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req, err := http.NewRequest(method, ts.URL+path, buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestListIncome(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	t.Run("unauthenticated_401", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/income", "", nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", resp.StatusCode)
		}
	})

	t.Run("returns_200_with_pagination", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/income", token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d, want 200", resp.StatusCode)
		}
		var env struct {
			Data []map[string]any `json:"data"`
			Meta v1.Meta          `json:"meta"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data == nil {
			t.Error("expected data array, got nil")
		}
		if env.Meta.PerPage <= 0 {
			t.Errorf("meta.per_page: got %d, want > 0", env.Meta.PerPage)
		}
	})
}

// TestListIncome_Paginated guards that pagination is pushed to the DB: a window
// is returned and Meta.Total reflects the true count (regression test for the
// fetch-all-then-slice bug).
func TestListIncome_Paginated(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	revID := revenueAccountID(t, db)
	depID := assetAccountID(t, db)

	for i := 0; i < 3; i++ {
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/income", token, map[string]any{
			"entry_date":      "2026-05-10",
			"description":     "pay",
			"amount":          "1000",
			"revenue_account": revID,
			"deposit_account": depID,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %d: status %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}

	var env struct {
		Data []map[string]any `json:"data"`
		Meta v1.Meta          `json:"meta"`
	}
	resp := doRequest(t, ts, http.MethodGet, "/api/v1/income?per_page=2&page=1", token, nil)
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if len(env.Data) != 2 {
		t.Errorf("page 1 per_page=2: got %d rows, want 2", len(env.Data))
	}
	if env.Meta.Total != 3 || env.Meta.TotalPages != 2 {
		t.Errorf("meta: got total=%d pages=%d, want total=3 pages=2", env.Meta.Total, env.Meta.TotalPages)
	}

	var env2 struct {
		Data []map[string]any `json:"data"`
		Meta v1.Meta          `json:"meta"`
	}
	resp2 := doRequest(t, ts, http.MethodGet, "/api/v1/income?per_page=2&page=2", token, nil)
	if err := json.NewDecoder(resp2.Body).Decode(&env2); err != nil {
		t.Fatalf("decode p2: %v", err)
	}
	resp2.Body.Close()
	if len(env2.Data) != 1 {
		t.Errorf("page 2: got %d rows, want 1", len(env2.Data))
	}
}

func TestGetIncome(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	revID := revenueAccountID(t, db)
	depID := assetAccountID(t, db)

	createBody := map[string]any{
		"entry_date":      "2026-05-10",
		"description":     "Bus contract payment",
		"amount":          "500000",
		"revenue_account": revID,
		"deposit_account": depID,
	}
	resp := doRequest(t, ts, http.MethodPost, "/api/v1/income", token, createBody)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}
	var created struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id := created.Data.ID

	t.Run("found_200", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/income/%d", id), token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d, want 200", resp.StatusCode)
		}
		var env struct {
			Data struct {
				ID             int    `json:"id"`
				EntryDate      string `json:"entry_date"`
				Amount         string `json:"amount"`
				RevenueAccount *struct {
					ID int `json:"id"`
				} `json:"revenue_account"`
				DepositAccount *struct {
					ID int `json:"id"`
				} `json:"deposit_account"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.ID != id {
			t.Errorf("id: got %d, want %d", env.Data.ID, id)
		}
		if env.Data.Amount != "500000" {
			t.Errorf("amount: got %q, want 500000", env.Data.Amount)
		}
		if env.Data.RevenueAccount == nil {
			t.Error("expected revenue_account, got nil")
		}
		if env.Data.DepositAccount == nil {
			t.Error("expected deposit_account, got nil")
		}
	})

	t.Run("not_found_404", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/income/999999", token, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})
}

func TestCreateIncome(t *testing.T) {
	ts, db := setupServer(t)

	revID := revenueAccountID(t, db)
	depID := assetAccountID(t, db)

	validBody := map[string]any{
		"entry_date":      "2026-05-10",
		"description":     "Charter bus payment",
		"amount":          "750000",
		"revenue_account": revID,
		"deposit_account": depID,
	}

	t.Run("success_201", func(t *testing.T) {
		token := adminToken(t, db)
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/income", token, validBody)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			var errEnv v1.ErrorEnvelope
			json.NewDecoder(resp.Body).Decode(&errEnv) //nolint:errcheck
			t.Fatalf("status: got %d, want 201: %v", resp.StatusCode, errEnv)
		}
		var env struct {
			Data struct {
				ID        int    `json:"id"`
				Amount    string `json:"amount"`
				Reference string `json:"reference"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.ID == 0 {
			t.Error("expected non-zero id")
		}
		if env.Data.Amount != "750000" {
			t.Errorf("amount: got %q, want 750000", env.Data.Amount)
		}
		if env.Data.Reference == "" {
			t.Error("expected reference to be set")
		}
	})

	t.Run("balanced_journal_created", func(t *testing.T) {
		token := adminToken(t, db)
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/income", token, map[string]any{
			"entry_date":      "2026-05-11",
			"description":     "Balance test",
			"amount":          "100000",
			"revenue_account": revID,
			"deposit_account": depID,
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create: status %d", resp.StatusCode)
		}
		var env struct {
			Data struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&env) //nolint:errcheck

		var totalDebit, totalCredit int
		err := db.QueryRow(
			`SELECT COALESCE(SUM(debit),0), COALESCE(SUM(credit),0) FROM journal_lines WHERE entry_id = ?`,
			env.Data.ID,
		).Scan(&totalDebit, &totalCredit)
		if err != nil {
			t.Fatalf("query lines: %v", err)
		}
		if totalDebit != totalCredit {
			t.Errorf("unbalanced journal: debit=%d credit=%d", totalDebit, totalCredit)
		}
		if totalDebit != 100000 {
			t.Errorf("debit: got %d, want 100000", totalDebit)
		}
	})

	t.Run("no_capability_403", func(t *testing.T) {
		token := noScopeToken(t, db)
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/income", token, validBody)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", resp.StatusCode)
		}
	})

	t.Run("missing_fields_422", func(t *testing.T) {
		token := adminToken(t, db)
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/income", token, map[string]any{
			"description": "Missing required fields",
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status: got %d, want 422", resp.StatusCode)
		}
		var errEnv v1.ErrorEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&errEnv); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if errEnv.Fields["entry_date"] == "" {
			t.Error("expected field error for entry_date")
		}
		if errEnv.Fields["amount"] == "" {
			t.Error("expected field error for amount")
		}
	})

	t.Run("unauthenticated_401", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/income", "", validBody)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", resp.StatusCode)
		}
	})
}

func TestCreateIncome_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	revID := revenueAccountID(t, db)
	depID := assetAccountID(t, db)

	body := map[string]any{
		"entry_date":      "2026-05-12",
		"description":     "Idempotency test income",
		"amount":          "300000",
		"revenue_account": revID,
		"deposit_account": depID,
	}

	idemKey := fmt.Sprintf("income-idem-%d", time.Now().UnixNano())

	resp1 := doRequestWithKey(t, ts, http.MethodPost, "/api/v1/income", token, idemKey, body)
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first request: status %d, want 201", resp1.StatusCode)
	}
	var env1 struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&env1); err != nil {
		t.Fatalf("decode first: %v", err)
	}

	resp2 := doRequestWithKey(t, ts, http.MethodPost, "/api/v1/income", token, idemKey, body)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("second request: status %d, want 201", resp2.StatusCode)
	}
	var env2 struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&env2); err != nil {
		t.Fatalf("decode second: %v", err)
	}

	if env1.Data.ID != env2.Data.ID {
		t.Errorf("idempotency: got different IDs: %d vs %d", env1.Data.ID, env2.Data.ID)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", env1.Data.ID).Scan(&count) //nolint:errcheck
	if count != 1 {
		t.Errorf("expected 1 DB row, got %d", count)
	}
}

func TestDeleteIncome(t *testing.T) {
	ts, db := setupServer(t)

	revID := revenueAccountID(t, db)
	depID := assetAccountID(t, db)

	createIncome := func(t *testing.T, token string) int {
		t.Helper()
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/income", token, map[string]any{
			"entry_date":      "2026-05-10",
			"description":     "To delete",
			"amount":          "200000",
			"revenue_account": revID,
			"deposit_account": depID,
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create: status %d", resp.StatusCode)
		}
		var env struct {
			Data struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&env) //nolint:errcheck
		return env.Data.ID
	}

	t.Run("success_204", func(t *testing.T) {
		token := adminToken(t, db)
		id := createIncome(t, token)

		resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/income/%d", id), token, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status: got %d, want 204", resp.StatusCode)
		}

		var count int
		db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE id = ?", id).Scan(&count) //nolint:errcheck
		if count != 0 {
			t.Errorf("expected entry deleted, got count %d", count)
		}
	})

	t.Run("no_capability_403", func(t *testing.T) {
		adminTok := adminToken(t, db)
		id := createIncome(t, adminTok)

		token := noScopeToken(t, db)
		resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/income/%d", id), token, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", resp.StatusCode)
		}
	})

	t.Run("not_found_404", func(t *testing.T) {
		token := adminToken(t, db)
		resp := doRequest(t, ts, http.MethodDelete, "/api/v1/income/999999", token, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})
}
