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
	"github.com/naufal/latasya-erp/internal/bill"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func setupServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)

	idem := v1.Idempotency(db)

	apiMux := http.NewServeMux()
	h := &v1bills.Handler{Bills: bill.New(db)}
	h.RegisterRoutes(apiMux, idem)

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

func TestListBills_Filters(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	id, _, _ := createBillFixture(t, ts, db, token)
	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()
	createBillFixture(t, ts, db, token) // stays draft

	t.Run("status_filter", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/bills?status=received", token, nil, nil)
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
		for _, b := range env.Data {
			if b.Status != "received" {
				t.Errorf("status filter leaked non-received bill: %s", b.Status)
			}
		}
		if env.Meta.Total < 1 {
			t.Errorf("total: got %d, want >= 1", env.Meta.Total)
		}
	})

	t.Run("search_filter", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/bills?search=nonexistent-xyz", token, nil, nil)
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
		if env.Meta.Total != 0 {
			t.Errorf("total: got %d, want 0", env.Meta.Total)
		}
	})
}

func TestGetBill_InvalidID(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/bills/not-a-number", token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestCreateBill_Auth(t *testing.T) {
	ts, db := setupServer(t)
	supplierID := seedSupplier(t, db)
	expenseAcct := accountID(t, db, "5-1001")
	body, err := json.Marshal(sampleBillBody(supplierID, expenseAcct))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	testutil.APIMatrix(t, ts, db, http.MethodPost, "/api/v1/bills", string(body), testutil.AuthMatrix{
		Anon:               http.StatusUnauthorized,
		ValidBearer:        http.StatusCreated,
		ExpiredBearer:      http.StatusUnauthorized,
		RevokedBearer:      http.StatusUnauthorized,
		ScopeMissingBearer: http.StatusForbidden,
	})
}

func TestCreateBill_InvalidBody(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/bills",
		bytes.NewBufferString(`{"contact_id": 1, "unexpected_field": true}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestCreateBill_ValidationErrors(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	supplierID := seedSupplier(t, db)
	expenseAcct := accountID(t, db, "5-1001")

	cases := map[string]func(map[string]any){
		"missing_contact_id":  func(b map[string]any) { b["contact_id"] = 0 },
		"missing_bill_date":   func(b map[string]any) { b["bill_date"] = "" },
		"missing_due_date":    func(b map[string]any) { b["due_date"] = "" },
		"invalid_tax_amount":  func(b map[string]any) { b["tax_amount"] = "not-a-number" },
		"negative_tax_amount": func(b map[string]any) { b["tax_amount"] = "-100" },
		"no_lines":            func(b map[string]any) { b["lines"] = []map[string]any{} },
		"invalid_quantity": func(b map[string]any) {
			b["lines"].([]map[string]any)[0]["quantity"] = "abc"
		},
		"invalid_unit_price": func(b map[string]any) {
			b["lines"].([]map[string]any)[0]["unit_price"] = "abc"
		},
		"missing_description": func(b map[string]any) {
			b["lines"].([]map[string]any)[0]["description"] = ""
		},
		"zero_unit_price": func(b map[string]any) {
			b["lines"].([]map[string]any)[0]["unit_price"] = "0"
		},
		"missing_account_id": func(b map[string]any) {
			b["lines"].([]map[string]any)[0]["account_id"] = 0
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			body := sampleBillBody(supplierID, expenseAcct)
			mutate(body)
			resp := doRequest(t, ts, http.MethodPost, "/api/v1/bills", token, body, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnprocessableEntity {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status: got %d (%s), want 422", resp.StatusCode, string(raw))
			}
		})
	}
}

func TestCreateBill_DBConstraintError(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	supplierID := seedSupplier(t, db)

	body := sampleBillBody(supplierID, 999999) // account_id doesn't exist
	resp := doRequest(t, ts, http.MethodPost, "/api/v1/bills", token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 422", resp.StatusCode, string(raw))
	}
}

func TestUpdateBill_Auth(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, supplierID, expenseAcct := createBillFixture(t, ts, db, token)
	body, err := json.Marshal(sampleBillBody(supplierID, expenseAcct))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	testutil.APIMatrix(t, ts, db, http.MethodPut, fmt.Sprintf("/api/v1/bills/%d", id), string(body), testutil.AuthMatrix{
		Anon:               http.StatusUnauthorized,
		ScopeMissingBearer: http.StatusForbidden,
	})
}

func TestUpdateBill_NotFound(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	supplierID := seedSupplier(t, db)
	expenseAcct := accountID(t, db, "5-1001")
	body := sampleBillBody(supplierID, expenseAcct)

	resp := doRequest(t, ts, http.MethodPut, "/api/v1/bills/999999", token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestUpdateBill_InvalidBody(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	req, err := http.NewRequest(http.MethodPut, ts.URL+fmt.Sprintf("/api/v1/bills/%d", id),
		bytes.NewBufferString(`{"unexpected_field": true}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestUpdateBill_ValidationFailed(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, supplierID, expenseAcct := createBillFixture(t, ts, db, token)

	body := sampleBillBody(supplierID, expenseAcct)
	body["due_date"] = ""
	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/bills/%d", id), token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status: got %d, want 422", resp.StatusCode)
	}
}

func TestUpdateBill_DBConstraintError(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, supplierID, _ := createBillFixture(t, ts, db, token)

	body := sampleBillBody(supplierID, 999999) // account_id doesn't exist
	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/bills/%d", id), token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 422", resp.StatusCode, string(raw))
	}
}

func TestUpdateBill_Success(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, supplierID, expenseAcct := createBillFixture(t, ts, db, token)

	body := sampleBillBody(supplierID, expenseAcct)
	body["notes"] = "updated bill notes"
	body["due_date"] = "2026-07-01"
	body["lines"].([]map[string]any)[0]["unit_price"] = "750000"

	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/bills/%d", id), token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 200", resp.StatusCode, string(raw))
	}
	var b model.Bill
	if err := json.NewDecoder(resp.Body).Decode(&b); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b.Notes != "updated bill notes" {
		t.Errorf("notes: got %q, want %q", b.Notes, "updated bill notes")
	}
	if b.DueDate != "2026-07-01" {
		t.Errorf("due_date: got %q, want %q", b.DueDate, "2026-07-01")
	}
	if b.Total != 750000 {
		t.Errorf("total: got %d, want 750000", b.Total)
	}
}

func TestDeleteBill(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	t.Run("auth", func(t *testing.T) {
		id, _, _ := createBillFixture(t, ts, db, token)
		testutil.APIMatrix(t, ts, db, http.MethodDelete, fmt.Sprintf("/api/v1/bills/%d", id), "", testutil.AuthMatrix{
			Anon:               http.StatusUnauthorized,
			ScopeMissingBearer: http.StatusForbidden,
		})
	})

	t.Run("not_found_bad_id", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodDelete, "/api/v1/bills/not-a-number", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})

	t.Run("not_found_missing", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodDelete, "/api/v1/bills/999999", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		id, _, _ := createBillFixture(t, ts, db, token)
		resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/bills/%d", id), token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status: got %d, want 204", resp.StatusCode)
		}

		get := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/bills/%d", id), token, nil, nil)
		defer get.Body.Close()
		if get.StatusCode != http.StatusNotFound {
			t.Errorf("post-delete get: got %d, want 404", get.StatusCode)
		}
	})
}

func TestReceiveBill_Auth(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	testutil.APIMatrix(t, ts, db, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), "", testutil.AuthMatrix{
		Anon:               http.StatusUnauthorized,
		ScopeMissingBearer: http.StatusForbidden,
	})
}

func TestReceiveBill_NotFound(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	t.Run("bad_id", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/bills/not-a-number/receive", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/bills/999999/receive", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})
}

func TestReceiveBill_AlreadyReceived(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	first := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	first.Body.Close()

	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", resp.StatusCode)
	}
}

func TestBillPayment_Auth(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)
	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	cashAcct := accountID(t, db, "1-1001")
	body, err := json.Marshal(map[string]any{
		"amount":          "100000",
		"payment_date":    "2026-05-20",
		"payment_account": cashAcct,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	testutil.APIMatrix(t, ts, db, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/payment", id), string(body), testutil.AuthMatrix{
		Anon:               http.StatusUnauthorized,
		ValidBearer:        http.StatusOK,
		ExpiredBearer:      http.StatusUnauthorized,
		RevokedBearer:      http.StatusUnauthorized,
		ScopeMissingBearer: http.StatusForbidden,
	})
}

func TestBillPayment_NotFound(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	t.Run("bad_id", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/bills/not-a-number/payment", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/bills/999999/payment", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})
}

func TestBillPayment_InvalidBody(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)

	req, err := http.NewRequest(http.MethodPost, ts.URL+fmt.Sprintf("/api/v1/bills/%d/payment", id),
		bytes.NewBufferString(`{"unexpected_field": true}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestBillPayment_ValidationErrors(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)
	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	cashAcct := accountID(t, db, "1-1001")
	path := fmt.Sprintf("/api/v1/bills/%d/payment", id)

	cases := map[string]map[string]any{
		"missing_amount": {
			"amount": "", "payment_date": "2026-05-15", "payment_account": cashAcct,
		},
		"non_numeric_amount": {
			"amount": "abc", "payment_date": "2026-05-15", "payment_account": cashAcct,
		},
		"negative_amount": {
			"amount": "-100", "payment_date": "2026-05-15", "payment_account": cashAcct,
		},
		"zero_amount": {
			"amount": "0", "payment_date": "2026-05-15", "payment_account": cashAcct,
		},
		"missing_payment_date": {
			"amount": "100000", "payment_date": "", "payment_account": cashAcct,
		},
		"missing_payment_account": {
			"amount": "100000", "payment_date": "2026-05-15", "payment_account": 0,
		},
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := doRequest(t, ts, http.MethodPost, path, token, body, nil)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnprocessableEntity {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("status: got %d (%s), want 422", resp.StatusCode, string(raw))
			}
		})
	}
}

func TestBillPayment_NotReceived(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token) // still draft

	cashAcct := accountID(t, db, "1-1001")
	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/payment", id), token, map[string]any{
		"amount":          "100000",
		"payment_date":    "2026-05-15",
		"payment_account": cashAcct,
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 409", resp.StatusCode, string(raw))
	}
}

func TestBillPayment_Overpayment(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token) // total 500000
	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	cashAcct := accountID(t, db, "1-1001")
	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/payment", id), token, map[string]any{
		"amount":          "600000",
		"payment_date":    "2026-05-15",
		"payment_account": cashAcct,
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 409", resp.StatusCode, string(raw))
	}
}

func TestBillPayment_PartialThenFull(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token) // total 500000
	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	cashAcct := accountID(t, db, "1-1001")
	path := fmt.Sprintf("/api/v1/bills/%d/payment", id)

	partial := doRequest(t, ts, http.MethodPost, path, token, map[string]any{
		"amount":          "200000",
		"payment_date":    "2026-05-15",
		"payment_account": cashAcct,
	}, nil)
	defer partial.Body.Close()
	if partial.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(partial.Body)
		t.Fatalf("partial: got %d (%s), want 200", partial.StatusCode, string(raw))
	}
	var afterPartial model.Bill
	if err := json.NewDecoder(partial.Body).Decode(&afterPartial); err != nil {
		t.Fatalf("decode partial: %v", err)
	}
	if afterPartial.Status != "partial" {
		t.Errorf("status after partial: got %q, want partial", afterPartial.Status)
	}
	if afterPartial.AmountPaid != 200000 {
		t.Errorf("amount_paid after partial: got %d, want 200000", afterPartial.AmountPaid)
	}

	final := doRequest(t, ts, http.MethodPost, path, token, map[string]any{
		"amount":          "300000",
		"payment_date":    "2026-05-16",
		"payment_account": cashAcct,
	}, nil)
	defer final.Body.Close()
	if final.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(final.Body)
		t.Fatalf("final: got %d (%s), want 200", final.StatusCode, string(raw))
	}
	var afterFinal model.Bill
	if err := json.NewDecoder(final.Body).Decode(&afterFinal); err != nil {
		t.Fatalf("decode final: %v", err)
	}
	if afterFinal.Status != "paid" {
		t.Errorf("status after final: got %q, want paid", afterFinal.Status)
	}
	if afterFinal.AmountPaid != 500000 {
		t.Errorf("amount_paid after final: got %d, want 500000", afterFinal.AmountPaid)
	}
}

func TestBillPayment_AlreadyPaid(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createBillFixture(t, ts, db, token)
	rec := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/bills/%d/receive", id), token, nil, nil)
	rec.Body.Close()

	cashAcct := accountID(t, db, "1-1001")
	path := fmt.Sprintf("/api/v1/bills/%d/payment", id)
	full := doRequest(t, ts, http.MethodPost, path, token, map[string]any{
		"amount":          "500000",
		"payment_date":    "2026-05-15",
		"payment_account": cashAcct,
	}, nil)
	full.Body.Close()

	resp := doRequest(t, ts, http.MethodPost, path, token, map[string]any{
		"amount":          "1",
		"payment_date":    "2026-05-16",
		"payment_account": cashAcct,
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 409", resp.StatusCode, string(raw))
	}
}
