package invoices_test

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
	v1invoices "github.com/naufal/latasya-erp/internal/api/v1/invoices"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func setupServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)

	apiMux := http.NewServeMux()
	h := &v1invoices.Handler{DB: db}
	idem := v1.Idempotency(db)
	apiMux.HandleFunc("GET /api/v1/invoices", h.List)
	apiMux.HandleFunc("GET /api/v1/invoices/{id}", h.Get)
	apiMux.Handle("POST /api/v1/invoices", idem(http.HandlerFunc(h.Create)))
	apiMux.Handle("PUT /api/v1/invoices/{id}", idem(http.HandlerFunc(h.Update)))
	apiMux.HandleFunc("DELETE /api/v1/invoices/{id}", h.Delete)
	apiMux.Handle("POST /api/v1/invoices/{id}/send", idem(http.HandlerFunc(h.Send)))
	apiMux.Handle("POST /api/v1/invoices/{id}/payment", idem(http.HandlerFunc(h.Payment)))
	apiMux.Handle("POST /api/v1/invoices/generate-recurring", idem(http.HandlerFunc(h.GenerateRecurring)))
	apiMux.HandleFunc("POST /api/v1/invoices/bulk-delete", h.BulkDelete)

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
	_, plaintext, err := model.CreateAPIToken(db, adminID, fmt.Sprintf("test-%d", time.Now().UnixNano()),
		[]string{model.CapInvoicesManage}, nil)
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

func seedContact(t *testing.T, db *sql.DB) int {
	t.Helper()
	res, err := db.Exec("INSERT INTO contacts (name, contact_type, is_active) VALUES (?, 'customer', 1)",
		fmt.Sprintf("Customer-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("seed contact: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func accountID(t *testing.T, db *sql.DB, code string) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = ?", code).Scan(&id); err != nil {
		t.Fatalf("get account %s: %v", code, err)
	}
	return id
}

func defaultInvoiceBody(contactID, revenueAccountID int) map[string]any {
	return map[string]any{
		"contact_id":   contactID,
		"invoice_date": "2026-05-10",
		"due_date":     "2026-06-10",
		"tax_amount":   "0",
		"notes":        "test invoice",
		"lines": []map[string]any{
			{
				"description": "School bus service",
				"quantity":    "1.00",
				"unit_price":  "500000",
				"account_id":  revenueAccountID,
			},
		},
	}
}

func doRequest(t *testing.T, ts *httptest.Server, method, path, token string, body any, headers map[string]string) *http.Response {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
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
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func createInvoice(t *testing.T, ts *httptest.Server, token string, body any) (int, string) {
	t.Helper()
	resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices", token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create invoice: status %d", resp.StatusCode)
	}
	var env struct {
		Data struct {
			ID            int    `json:"id"`
			InvoiceNumber string `json:"invoice_number"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env.Data.ID, env.Data.InvoiceNumber
}

func TestListInvoices(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")

	createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))
	createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/invoices", token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data []json.RawMessage `json:"data"`
		Meta v1.Meta           `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Meta.Total < 2 {
		t.Errorf("total: got %d, want >= 2", env.Meta.Total)
	}
}

func TestGetInvoice(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")

	id, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))

	t.Run("found", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/invoices/%d", id), token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d, want 200", resp.StatusCode)
		}
		var env struct {
			Data struct {
				ID        int               `json:"id"`
				Status    string            `json:"status"`
				Total     string            `json:"total"`
				AmountDue string            `json:"amount_due"`
				Lines     []json.RawMessage `json:"lines"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.ID != id {
			t.Errorf("id: got %d want %d", env.Data.ID, id)
		}
		if env.Data.Total != "500000" {
			t.Errorf("total: got %q want %q", env.Data.Total, "500000")
		}
		if env.Data.AmountDue != "500000" {
			t.Errorf("amount_due: got %q want %q", env.Data.AmountDue, "500000")
		}
		if len(env.Data.Lines) == 0 {
			t.Errorf("expected lines, got none")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/invoices/999999", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d want 404", resp.StatusCode)
		}
	})
}

func TestCreateInvoice(t *testing.T) {
	ts, db := setupServer(t)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")

	t.Run("success", func(t *testing.T) {
		token := adminToken(t, db)
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices", token, defaultInvoiceBody(cid, rev), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status: got %d want 201", resp.StatusCode)
		}
		var env struct {
			Data struct {
				ID            int    `json:"id"`
				InvoiceNumber string `json:"invoice_number"`
				Status        string `json:"status"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.InvoiceNumber == "" {
			t.Error("expected invoice_number, got empty")
		}
		if env.Data.Status != "draft" {
			t.Errorf("status: got %q want draft", env.Data.Status)
		}
	})

	t.Run("no_capability_403", func(t *testing.T) {
		token := noScopeToken(t, db)
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices", token, defaultInvoiceBody(cid, rev), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status: got %d want 403", resp.StatusCode)
		}
	})

	t.Run("validation_failed", func(t *testing.T) {
		token := adminToken(t, db)
		body := map[string]any{"contact_id": 0, "lines": []any{}}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices", token, body, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status: got %d want 422", resp.StatusCode)
		}
	})
}

func TestCreateInvoice_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")
	body := defaultInvoiceBody(cid, rev)

	headers := map[string]string{"Idempotency-Key": "create-key-1"}

	r1 := doRequest(t, ts, http.MethodPost, "/api/v1/invoices", token, body, headers)
	b1, _ := readBody(r1)
	r1.Body.Close()
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("first: %d", r1.StatusCode)
	}

	r2 := doRequest(t, ts, http.MethodPost, "/api/v1/invoices", token, body, headers)
	b2, _ := readBody(r2)
	r2.Body.Close()
	if r2.StatusCode != http.StatusCreated {
		t.Fatalf("second: %d", r2.StatusCode)
	}

	if string(b1) != string(b2) {
		t.Errorf("responses differ:\n%s\n---\n%s", b1, b2)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM invoices").Scan(&count)
	if count != 1 {
		t.Errorf("invoices count: got %d, want 1", count)
	}
}

func TestSendInvoice(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")
	id, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))

	t.Run("success", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d want 200", resp.StatusCode)
		}
		var env struct {
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.Status != "sent" {
			t.Errorf("status: got %q want sent", env.Data.Status)
		}
	})

	t.Run("already_sent_409", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status: got %d want 409", resp.StatusCode)
		}
	})
}

func TestSendInvoice_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")
	id, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))

	headers := map[string]string{"Idempotency-Key": "send-key-1"}

	r1 := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), token, nil, headers)
	b1, _ := readBody(r1)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first: %d", r1.StatusCode)
	}

	r2 := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), token, nil, headers)
	b2, _ := readBody(r2)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("second: %d", r2.StatusCode)
	}
	if string(b1) != string(b2) {
		t.Errorf("responses differ")
	}

	var jeCount int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE source_type = 'invoice'").Scan(&jeCount)
	if jeCount != 1 {
		t.Errorf("journal entries: got %d, want 1", jeCount)
	}
}

func TestInvoicePayment(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")
	bank := accountID(t, db, "1-1002")

	id, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))
	r := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), token, nil, nil)
	r.Body.Close()

	t.Run("partial_payment", func(t *testing.T) {
		body := map[string]any{
			"amount":          "200000",
			"payment_date":    "2026-05-15",
			"payment_account": bank,
		}
		resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/payment", id), token, body, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d want 200", resp.StatusCode)
		}
		var env struct {
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&env)
		if env.Data.Status != "partial" {
			t.Errorf("status: got %q want partial", env.Data.Status)
		}
	})

	t.Run("full_payment", func(t *testing.T) {
		body := map[string]any{
			"amount":          "300000",
			"payment_date":    "2026-05-20",
			"payment_account": bank,
		}
		resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/payment", id), token, body, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d want 200", resp.StatusCode)
		}
		var env struct {
			Data struct {
				Status string `json:"status"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&env)
		if env.Data.Status != "paid" {
			t.Errorf("status: got %q want paid", env.Data.Status)
		}
	})

	t.Run("over_payment_422", func(t *testing.T) {
		id2, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))
		r := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id2), token, nil, nil)
		r.Body.Close()

		body := map[string]any{
			"amount":          "9999999",
			"payment_date":    "2026-05-15",
			"payment_account": bank,
		}
		resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/payment", id2), token, body, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status: got %d want 422", resp.StatusCode)
		}
	})
}

func TestInvoicePayment_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")
	bank := accountID(t, db, "1-1002")

	id, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))
	r := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), token, nil, nil)
	r.Body.Close()

	body := map[string]any{
		"amount":          "100000",
		"payment_date":    "2026-05-15",
		"payment_account": bank,
	}
	headers := map[string]string{"Idempotency-Key": "pay-key-1"}

	r1 := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/payment", id), token, body, headers)
	b1, _ := readBody(r1)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("first: %d", r1.StatusCode)
	}

	r2 := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/payment", id), token, body, headers)
	b2, _ := readBody(r2)
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("second: %d", r2.StatusCode)
	}
	if string(b1) != string(b2) {
		t.Errorf("responses differ")
	}

	var payCount int
	db.QueryRow("SELECT COUNT(*) FROM payments WHERE reference_id = ? AND payment_type = 'invoice'", id).Scan(&payCount)
	if payCount != 1 {
		t.Errorf("payments: got %d, want 1", payCount)
	}
}

func TestUpdateInvoice_NonDraft(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")
	id, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))
	r := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), token, nil, nil)
	r.Body.Close()

	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/invoices/%d", id), token, defaultInvoiceBody(cid, rev), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d want 409", resp.StatusCode)
	}
}

func TestDeleteInvoice_NonDraft(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")
	id, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))
	r := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/invoices/%d/send", id), token, nil, nil)
	r.Body.Close()

	resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/invoices/%d", id), token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d want 409", resp.StatusCode)
	}
}

func TestDeleteInvoice_Draft(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")
	id, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))

	resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/invoices/%d", id), token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", resp.StatusCode)
	}
}

func TestGenerateRecurring(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")

	body := defaultInvoiceBody(cid, rev)
	body["invoice_date"] = "2020-01-10"
	body["due_date"] = "2020-01-20"
	createInvoice(t, ts, token, body)

	t.Run("success", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices/generate-recurring", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d want 200", resp.StatusCode)
		}
		var env struct {
			Data struct {
				Created int `json:"created"`
				Skipped int `json:"skipped"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.Created < 1 {
			t.Errorf("created: got %d want >= 1", env.Data.Created)
		}
	})

	t.Run("no_capability_403", func(t *testing.T) {
		tok := noScopeToken(t, db)
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices/generate-recurring", tok, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status: got %d want 403", resp.StatusCode)
		}
	})
}

func TestBulkDelete(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	cid := seedContact(t, db)
	rev := accountID(t, db, "4-1001")

	id1, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))
	id2, _ := createInvoice(t, ts, token, defaultInvoiceBody(cid, rev))

	t.Run("success", func(t *testing.T) {
		body := map[string]any{"ids": []int{id1, id2}}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices/bulk-delete", token, body, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d want 200", resp.StatusCode)
		}
		var env struct {
			Data struct {
				Deleted int   `json:"deleted"`
				Skipped []int `json:"skipped"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Data.Deleted != 2 {
			t.Errorf("deleted: got %d want 2", env.Data.Deleted)
		}
	})

	t.Run("empty_422", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices/bulk-delete", token, map[string]any{"ids": []int{}}, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status: got %d want 422", resp.StatusCode)
		}
	})

	t.Run("no_capability_403", func(t *testing.T) {
		tok := noScopeToken(t, db)
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/invoices/bulk-delete", tok, map[string]any{"ids": []int{1}}, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status: got %d want 403", resp.StatusCode)
		}
	})
}

func readBody(r *http.Response) ([]byte, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}
