package bills_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	v1bills "github.com/naufal/latasya-erp/internal/api/v1/bills"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func setupServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)

	idem := v1.Idempotency(db)

	apiMux := http.NewServeMux()
	h := &v1bills.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/bills", h.List)
	apiMux.HandleFunc("GET /api/v1/bills/{id}", h.Get)
	apiMux.Handle("POST /api/v1/bills", idem(http.HandlerFunc(h.Create)))
	apiMux.Handle("PUT /api/v1/bills/{id}", idem(http.HandlerFunc(h.Update)))
	apiMux.HandleFunc("DELETE /api/v1/bills/{id}", h.Delete)
	apiMux.Handle("POST /api/v1/bills/{id}/receive", idem(http.HandlerFunc(h.Receive)))
	apiMux.Handle("POST /api/v1/bills/{id}/payment", idem(http.HandlerFunc(h.Payment)))

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
		[]string{model.CapBillsManage}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return plaintext
}

func seedSupplier(t *testing.T, db *sql.DB) int {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO contacts (name, contact_type, is_active) VALUES (?, 'supplier', 1)",
		fmt.Sprintf("Supplier %d", time.Now().UnixNano()),
	)
	if err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func accountID(t *testing.T, db *sql.DB, code string) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM accounts WHERE code = ?", code).Scan(&id); err != nil {
		t.Fatalf("account %s: %v", code, err)
	}
	return id
}

func doRequest(t *testing.T, ts *httptest.Server, method, path, token string, body any, headers map[string]string) *http.Response {
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
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func sampleBillBody(supplierID, expenseAcct int) map[string]any {
	return map[string]any{
		"contact_id": supplierID,
		"bill_date":  "2026-05-10",
		"due_date":   "2026-06-10",
		"tax_amount": "0",
		"notes":      "test bill",
		"lines": []map[string]any{
			{
				"description": "Fuel purchase",
				"quantity":    "1.00",
				"unit_price":  "500000",
				"account_id":  expenseAcct,
			},
		},
	}
}

func createBillFixture(t *testing.T, ts *httptest.Server, db *sql.DB, token string) (int, int, int) {
	t.Helper()
	supplierID := seedSupplier(t, db)
	expenseAcct := accountID(t, db, "5-1001")

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/bills", token, sampleBillBody(supplierID, expenseAcct), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create bill: got %d, body=%s", resp.StatusCode, string(body))
	}
	var created model.Bill
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return created.ID, supplierID, expenseAcct
}

func TestListBills(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	createBillFixture(t, ts, db, token)
	createBillFixture(t, ts, db, token)

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/bills", token, nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data []model.Bill `json:"data"`
		Meta v1.Meta      `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Meta.Total < 2 {
		t.Errorf("total: got %d, want >= 2", env.Meta.Total)
	}
}

func TestGetBill(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	id, _, _ := createBillFixture(t, ts, db, token)

	t.Run("found", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/bills/%d", id), token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d, want 200", resp.StatusCode)
		}
		var b model.Bill
		if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if b.ID != id {
			t.Errorf("id: got %d, want %d", b.ID, id)
		}
		if len(b.Lines) != 1 {
			t.Errorf("lines: got %d, want 1", len(b.Lines))
		}
	})

	t.Run("not_found", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/bills/999999", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})
}

func TestCreateBill(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	supplierID := seedSupplier(t, db)
	expenseAcct := accountID(t, db, "5-1001")

	body := sampleBillBody(supplierID, expenseAcct)
	resp := doRequest(t, ts, http.MethodPost, "/api/v1/bills", token, body, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 201", resp.StatusCode, string(raw))
	}
	var b model.Bill
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.ID == 0 {
		t.Errorf("id: got 0")
	}
	if b.BillNumber == "" {
		t.Errorf("bill_number missing")
	}
	if b.Status != "draft" {
		t.Errorf("status: got %q, want draft", b.Status)
	}
	if b.Total != 500000 {
		t.Errorf("total: got %d, want 500000", b.Total)
	}
}

func TestCreateBill_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	supplierID := seedSupplier(t, db)
	expenseAcct := accountID(t, db, "5-1001")
	body := sampleBillBody(supplierID, expenseAcct)
	key := "bill-idem-" + fmt.Sprint(time.Now().UnixNano())

	resp1 := doRequest(t, ts, http.MethodPost, "/api/v1/bills", token, body,
		map[string]string{"Idempotency-Key": key})
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first: got %d, want 201", resp1.StatusCode)
	}
	body1, _ := io.ReadAll(resp1.Body)

	resp2 := doRequest(t, ts, http.MethodPost, "/api/v1/bills", token, body,
		map[string]string{"Idempotency-Key": key})
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)

	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("second: got %d, want 201", resp2.StatusCode)
	}
	if !bytes.Equal(body1, body2) {
		t.Errorf("idempotent replay must return identical body")
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM bills").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("bills count: got %d, want 1 (idempotency must dedupe)", count)
	}
}

func TestReceiveBill(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 200", resp.StatusCode, string(raw))
	}
	var b model.Bill
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.Status != "received" {
		t.Errorf("status: got %q, want received", b.Status)
	}
	if b.JournalID == nil {
		t.Errorf("journal_id: expected non-nil")
	}
}

func TestReceiveBill_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	key := "recv-idem-" + fmt.Sprint(time.Now().UnixNano())
	path := fmt.Sprintf("/api/v1/bills/%d/receive", id)

	resp1 := doRequest(t, ts, http.MethodPost, path, token, nil,
		map[string]string{"Idempotency-Key": key})
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first: got %d, want 200", resp1.StatusCode)
	}
	body1, _ := io.ReadAll(resp1.Body)

	resp2 := doRequest(t, ts, http.MethodPost, path, token, nil,
		map[string]string{"Idempotency-Key": key})
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second: got %d, want 200", resp2.StatusCode)
	}
	if !bytes.Equal(body1, body2) {
		t.Errorf("idempotent replay must return identical body")
	}

	var journalCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE source_type = 'bill'").Scan(&journalCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if journalCount != 1 {
		t.Errorf("journal entries: got %d, want 1", journalCount)
	}
}

func TestBillPayment(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	cashAcct := accountID(t, db, "1-1001")
	body := map[string]any{
		"amount":          "500000",
		"payment_date":    "2026-05-15",
		"payment_account": cashAcct,
	}

	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/payment", id), token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 200", resp.StatusCode, string(raw))
	}
	var b model.Bill
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.Status != "paid" {
		t.Errorf("status: got %q, want paid", b.Status)
	}
	if b.AmountPaid != 500000 {
		t.Errorf("amount_paid: got %d, want 500000", b.AmountPaid)
	}
}

func TestBillPayment_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	cashAcct := accountID(t, db, "1-1001")
	body := map[string]any{
		"amount":          "200000",
		"payment_date":    "2026-05-15",
		"payment_account": cashAcct,
	}
	key := "pay-idem-" + fmt.Sprint(time.Now().UnixNano())
	path := fmt.Sprintf("/api/v1/bills/%d/payment", id)

	resp1 := doRequest(t, ts, http.MethodPost, path, token, body,
		map[string]string{"Idempotency-Key": key})
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp1.Body)
		t.Fatalf("first: got %d (%s), want 200", resp1.StatusCode, string(raw))
	}
	b1, _ := io.ReadAll(resp1.Body)

	resp2 := doRequest(t, ts, http.MethodPost, path, token, body,
		map[string]string{"Idempotency-Key": key})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second: got %d, want 200", resp2.StatusCode)
	}
	b2, _ := io.ReadAll(resp2.Body)
	if !bytes.Equal(b1, b2) {
		t.Errorf("idempotent replay must return identical body")
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM payments WHERE reference_id = ?", id).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("payments: got %d, want 1", count)
	}
}

func TestUpdateBill_NonDraft(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, supplierID, expenseAcct := createBillFixture(t, ts, db, token)

	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	body := sampleBillBody(supplierID, expenseAcct)
	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/bills/%d", id), token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", resp.StatusCode)
	}
}

func TestDeleteBill_NonDraft(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/bills/%d", id), token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", resp.StatusCode)
	}
}
