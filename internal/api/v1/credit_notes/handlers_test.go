package creditnotes_test

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
	creditnotes "github.com/naufal/latasya-erp/internal/api/v1/credit_notes"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func setupServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)

	idem := v1.Idempotency(db)

	apiMux := http.NewServeMux()
	h := &creditnotes.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/credit-notes", h.List)
	apiMux.HandleFunc("GET /api/v1/credit-notes/{id}", h.Get)
	apiMux.Handle("POST /api/v1/credit-notes", idem(http.HandlerFunc(h.Create)))
	apiMux.Handle("PUT /api/v1/credit-notes/{id}", idem(http.HandlerFunc(h.Update)))
	apiMux.HandleFunc("DELETE /api/v1/credit-notes/{id}", h.Delete)
	apiMux.Handle("POST /api/v1/credit-notes/{id}/issue", idem(http.HandlerFunc(h.Issue)))
	apiMux.Handle("POST /api/v1/credit-notes/{id}/void", idem(http.HandlerFunc(h.Void)))

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

func seedCustomer(t *testing.T, db *sql.DB) int {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO contacts (name, contact_type, is_active) VALUES (?, 'customer', 1)",
		fmt.Sprintf("Customer %d", time.Now().UnixNano()),
	)
	if err != nil {
		t.Fatalf("seed customer: %v", err)
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

func sampleCNBody(customerID, revenueAcct int) map[string]any {
	return map[string]any{
		"contact_id": customerID,
		"cn_date":    "2026-05-12",
		"reason":     model.CreditNoteReasonCancellation,
		"tax_amount": "0",
		"notes":      "test credit note",
		"lines": []map[string]any{
			{
				"description": "Refund for service",
				"quantity":    "1.00",
				"unit_price":  "100000",
				"account_id":  revenueAcct,
			},
		},
	}
}

func createCNFixture(t *testing.T, ts *httptest.Server, db *sql.DB, token string) (int, int, int) {
	t.Helper()
	customerID := seedCustomer(t, db)
	revenueAcct := accountID(t, db, "4-1001")

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/credit-notes", token, sampleCNBody(customerID, revenueAcct), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create CN: got %d, body=%s", resp.StatusCode, string(body))
	}
	var created model.CreditNote
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return created.ID, customerID, revenueAcct
}

func TestListCreditNotes(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	createCNFixture(t, ts, db, token)
	createCNFixture(t, ts, db, token)

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/credit-notes", token, nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data []model.CreditNote `json:"data"`
		Meta v1.Meta            `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Meta.Total < 2 {
		t.Errorf("total: got %d, want >= 2", env.Meta.Total)
	}
}

func TestGetCreditNote(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createCNFixture(t, ts, db, token)

	t.Run("found", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/credit-notes/%d", id), token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d, want 200", resp.StatusCode)
		}
		var cn model.CreditNote
		if err := json.NewDecoder(resp.Body).Decode(&cn); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if cn.ID != id {
			t.Errorf("id: got %d, want %d", cn.ID, id)
		}
		if len(cn.Lines) != 1 {
			t.Errorf("lines: got %d, want 1", len(cn.Lines))
		}
	})

	t.Run("not_found", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/credit-notes/999999", token, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})
}

func TestCreateCreditNote(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	customerID := seedCustomer(t, db)
	revenueAcct := accountID(t, db, "4-1001")

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/credit-notes", token,
		sampleCNBody(customerID, revenueAcct), nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 201", resp.StatusCode, string(raw))
	}
	var cn model.CreditNote
	if err := json.NewDecoder(resp.Body).Decode(&cn); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cn.ID == 0 {
		t.Errorf("id: got 0")
	}
	if cn.CNNumber == "" {
		t.Errorf("cn_number missing")
	}
	if cn.Status != model.StatusDraft {
		t.Errorf("status: got %q, want draft", cn.Status)
	}
	if cn.Total != 100000 {
		t.Errorf("total: got %d, want 100000", cn.Total)
	}
}

func TestCreateCreditNote_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	customerID := seedCustomer(t, db)
	revenueAcct := accountID(t, db, "4-1001")
	body := sampleCNBody(customerID, revenueAcct)
	key := "cn-idem-" + fmt.Sprint(time.Now().UnixNano())

	resp1 := doRequest(t, ts, http.MethodPost, "/api/v1/credit-notes", token, body,
		map[string]string{"Idempotency-Key": key})
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first: got %d, want 201", resp1.StatusCode)
	}
	body1, _ := io.ReadAll(resp1.Body)

	resp2 := doRequest(t, ts, http.MethodPost, "/api/v1/credit-notes", token, body,
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
	if err := db.QueryRow("SELECT COUNT(*) FROM credit_notes").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("credit_notes count: got %d, want 1", count)
	}
}

func TestIssueCreditNote(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createCNFixture(t, ts, db, token)

	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/credit-notes/%d/issue", id), token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 200", resp.StatusCode, string(raw))
	}
	var cn model.CreditNote
	if err := json.NewDecoder(resp.Body).Decode(&cn); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cn.Status != model.StatusIssued {
		t.Errorf("status: got %q, want issued", cn.Status)
	}
	if cn.JournalID == nil {
		t.Errorf("journal_id: expected non-nil")
	}
}

func TestIssueCreditNote_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createCNFixture(t, ts, db, token)

	key := "issue-idem-" + fmt.Sprint(time.Now().UnixNano())
	path := fmt.Sprintf("/api/v1/credit-notes/%d/issue", id)

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

	var jeCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE source_type = ?", model.SourceCreditNote).Scan(&jeCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if jeCount != 1 {
		t.Errorf("journal entries: got %d, want 1", jeCount)
	}
}

func TestVoidCreditNote(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createCNFixture(t, ts, db, token)

	issue := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/credit-notes/%d/issue", id), token, nil, nil)
	issue.Body.Close()

	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/credit-notes/%d/void", id), token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: got %d (%s), want 200", resp.StatusCode, string(raw))
	}
	var cn model.CreditNote
	if err := json.NewDecoder(resp.Body).Decode(&cn); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cn.Status != model.StatusVoid {
		t.Errorf("status: got %q, want void", cn.Status)
	}
}

func TestVoidCreditNote_Idempotency(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createCNFixture(t, ts, db, token)

	issue := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/credit-notes/%d/issue", id), token, nil, nil)
	issue.Body.Close()

	key := "void-idem-" + fmt.Sprint(time.Now().UnixNano())
	path := fmt.Sprintf("/api/v1/credit-notes/%d/void", id)

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
}

func TestUpdateCreditNote_NonDraft(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, customerID, revenueAcct := createCNFixture(t, ts, db, token)

	issue := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/credit-notes/%d/issue", id), token, nil, nil)
	issue.Body.Close()

	body := sampleCNBody(customerID, revenueAcct)
	resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/credit-notes/%d", id), token, body, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", resp.StatusCode)
	}
}

func TestVoidCreditNote_AlreadyVoided(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id, _, _ := createCNFixture(t, ts, db, token)

	issue := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/credit-notes/%d/issue", id), token, nil, nil)
	issue.Body.Close()
	void1 := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/credit-notes/%d/void", id), token, nil, nil)
	void1.Body.Close()

	resp := doRequest(t, ts, http.MethodPost, fmt.Sprintf("/api/v1/credit-notes/%d/void", id), token, nil, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status: got %d, want 409", resp.StatusCode)
	}
}
