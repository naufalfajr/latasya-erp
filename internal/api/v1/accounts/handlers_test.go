package accounts_test

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
	"github.com/naufal/latasya-erp/internal/api/v1/accounts"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &accounts.Handler{DB: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/accounts", h.List)
	mux.HandleFunc("GET /api/v1/accounts/{id}", h.Get)
	mux.HandleFunc("POST /api/v1/accounts", h.Create)
	mux.HandleFunc("PUT /api/v1/accounts/{id}", h.Update)
	mux.HandleFunc("DELETE /api/v1/accounts/{id}", h.Delete)
	ts := httptest.NewServer(v1.BearerOrCookie(db)(mux))
	t.Cleanup(ts.Close)
	return ts
}

func adminBearerToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("get admin user: %v", err)
	}
	_, plaintext, err := model.CreateAPIToken(db, adminID,
		fmt.Sprintf("test-admin-%d", time.Now().UnixNano()),
		[]string{model.CapAccountsManage}, nil)
	if err != nil {
		t.Fatalf("create admin token: %v", err)
	}
	return plaintext
}

func doRequest(t *testing.T, ts *httptest.Server, method, path, bearer string, body any) *http.Response {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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

func TestListAccounts(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	testutil.APIMatrix(t, ts, db, http.MethodGet, "/api/v1/accounts", "", testutil.AuthMatrix{
		Anon:          http.StatusUnauthorized,
		ValidBearer:   http.StatusOK,
		ExpiredBearer: http.StatusUnauthorized,
		RevokedBearer: http.StatusUnauthorized,
	})

	token := adminBearerToken(t, db)

	t.Run("data array and meta present", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/accounts", token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var envelope struct {
			Data []model.Account `json:"data"`
			Meta v1.Meta         `json:"meta"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if envelope.Data == nil {
			t.Error("expected data array, got nil")
		}
		if envelope.Meta.PerPage <= 0 {
			t.Error("expected meta.per_page > 0")
		}
		if envelope.Meta.Total != len(envelope.Data) {
			t.Errorf("meta.total %d != len(data) %d", envelope.Meta.Total, len(envelope.Data))
		}
	})

	t.Run("filter by type", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/accounts?type=asset", token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var envelope struct {
			Data []model.Account `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		for _, a := range envelope.Data {
			if a.AccountType != "asset" {
				t.Errorf("unexpected account_type %q in asset-filtered results", a.AccountType)
			}
		}
	})
}

func TestGetAccount(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminBearerToken(t, db)

	var existingID int
	if err := db.QueryRow("SELECT id FROM accounts LIMIT 1").Scan(&existingID); err != nil {
		t.Fatalf("find existing account: %v", err)
	}

	t.Run("existing account returns 200", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/accounts/%d", existingID), token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var envelope struct {
			Data model.Account `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if envelope.Data.ID != existingID {
			t.Errorf("expected account ID %d, got %d", existingID, envelope.Data.ID)
		}
	})

	t.Run("missing account returns 404", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/accounts/999999", token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/accounts/%d", existingID), "", nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestCreateAccount(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminBearerToken(t, db)

	testutil.APIMatrix(t, ts, db, http.MethodPost, "/api/v1/accounts",
		`{"code":"9-MATRIX","name":"Matrix Test","account_type":"asset","normal_balance":"debit"}`,
		testutil.AuthMatrix{
			Anon:               http.StatusUnauthorized,
			ValidBearer:        http.StatusCreated,
			ExpiredBearer:      http.StatusUnauthorized,
			RevokedBearer:      http.StatusUnauthorized,
			ScopeMissingBearer: http.StatusForbidden,
		})

	t.Run("valid body returns 201 with data", func(t *testing.T) {
		body := map[string]any{
			"code":           "9-VALID-1",
			"name":           "Valid Account",
			"account_type":   "asset",
			"normal_balance": "debit",
		}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/accounts", token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			var errBody map[string]any
			json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
			t.Fatalf("expected 201, got %d: %v", resp.StatusCode, errBody)
		}
		var envelope struct {
			Data model.Account `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if envelope.Data.Code != "9-VALID-1" {
			t.Errorf("expected code 9-VALID-1, got %q", envelope.Data.Code)
		}
		if envelope.Data.ID == 0 {
			t.Error("expected non-zero ID in response")
		}
	})

	t.Run("missing required fields returns 422", func(t *testing.T) {
		body := map[string]any{
			"name": "Missing Code and Type",
		}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/accounts", token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d", resp.StatusCode)
		}
		var errEnv v1.ErrorEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&errEnv); err != nil {
			t.Fatalf("decode error envelope: %v", err)
		}
		if errEnv.Code != v1.CodeValidationFailed {
			t.Errorf("expected code %q, got %q", v1.CodeValidationFailed, errEnv.Code)
		}
		if errEnv.Fields["code"] == "" {
			t.Error("expected 'code' field error")
		}
		if errEnv.Fields["account_type"] == "" {
			t.Error("expected 'account_type' field error")
		}
	})

	t.Run("is_active defaults to true", func(t *testing.T) {
		body := map[string]any{
			"code":           "9-DEFACTIVE",
			"name":           "Default Active",
			"account_type":   "expense",
			"normal_balance": "debit",
		}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/accounts", token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
		var envelope struct {
			Data model.Account `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !envelope.Data.IsActive {
			t.Error("expected is_active=true by default")
		}
	})
}

func TestUpdateAccount(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminBearerToken(t, db)

	result, err := db.Exec("INSERT INTO accounts (code, name, account_type, normal_balance, is_active) VALUES ('UPD-1', 'Update Me', 'asset', 'debit', 1)")
	if err != nil {
		t.Fatalf("insert test account: %v", err)
	}
	testID, _ := result.LastInsertId()

	testutil.APIMatrix(t, ts, db, http.MethodPut,
		fmt.Sprintf("/api/v1/accounts/%d", testID),
		`{"code":"UPD-1","name":"Updated","account_type":"asset","normal_balance":"debit"}`,
		testutil.AuthMatrix{
			Anon:               http.StatusUnauthorized,
			ValidBearer:        http.StatusOK,
			ExpiredBearer:      http.StatusUnauthorized,
			RevokedBearer:      http.StatusUnauthorized,
			ScopeMissingBearer: http.StatusForbidden,
		})

	t.Run("valid update returns 200 with data", func(t *testing.T) {
		body := map[string]any{
			"code":           "UPD-1",
			"name":           "Updated Name",
			"account_type":   "asset",
			"normal_balance": "debit",
			"description":    "updated description",
		}
		resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/accounts/%d", testID), token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var envelope struct {
			Data model.Account `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if envelope.Data.Name != "Updated Name" {
			t.Errorf("expected name 'Updated Name', got %q", envelope.Data.Name)
		}
		if envelope.Data.Description != "updated description" {
			t.Errorf("expected description 'updated description', got %q", envelope.Data.Description)
		}
	})

	t.Run("missing account returns 404", func(t *testing.T) {
		body := map[string]any{
			"code":           "X",
			"name":           "X",
			"account_type":   "asset",
			"normal_balance": "debit",
		}
		resp := doRequest(t, ts, http.MethodPut, "/api/v1/accounts/999999", token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid fields returns 422", func(t *testing.T) {
		body := map[string]any{
			"code":           "",
			"name":           "No Code",
			"account_type":   "asset",
			"normal_balance": "debit",
		}
		resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/accounts/%d", testID), token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %d", resp.StatusCode)
		}
	})
}

func TestDeleteAccount(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminBearerToken(t, db)

	result, err := db.Exec("INSERT INTO accounts (code, name, account_type, normal_balance, is_active, is_system) VALUES ('DEL-AUTH', 'Auth Test Delete', 'asset', 'debit', 1, 0)")
	if err != nil {
		t.Fatalf("insert auth test account: %v", err)
	}
	authTestID, _ := result.LastInsertId()

	testutil.APIMatrix(t, ts, db, http.MethodDelete,
		fmt.Sprintf("/api/v1/accounts/%d", authTestID),
		"",
		testutil.AuthMatrix{
			Anon:               http.StatusUnauthorized,
			ScopeMissingBearer: http.StatusForbidden,
		})

	t.Run("delete user account returns 204", func(t *testing.T) {
		r, err := db.Exec("INSERT INTO accounts (code, name, account_type, normal_balance, is_active, is_system) VALUES ('DEL-OK', 'Delete OK', 'asset', 'debit', 1, 0)")
		if err != nil {
			t.Fatalf("insert account: %v", err)
		}
		delID, _ := r.LastInsertId()

		resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/accounts/%d", delID), token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", resp.StatusCode)
		}
	})

	t.Run("missing account returns 404", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodDelete, "/api/v1/accounts/999999", token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("system account returns 409", func(t *testing.T) {
		r, err := db.Exec("INSERT INTO accounts (code, name, account_type, normal_balance, is_active, is_system) VALUES ('SYS-DEL', 'System Del', 'asset', 'debit', 1, 1)")
		if err != nil {
			t.Fatalf("insert system account: %v", err)
		}
		sysID, _ := r.LastInsertId()

		resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/accounts/%d", sysID), token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("expected 409, got %d", resp.StatusCode)
		}
		var errEnv v1.ErrorEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&errEnv); err != nil {
			t.Fatalf("decode error envelope: %v", err)
		}
		if errEnv.Code != v1.CodeConflict {
			t.Errorf("expected code %q, got %q", v1.CodeConflict, errEnv.Code)
		}
	})
}
