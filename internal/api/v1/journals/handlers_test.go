package journals_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/api/v1/journals"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	h := &journals.Handler{DB: db}
	idem := v1.Idempotency(db)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/journals", h.List)
	mux.HandleFunc("GET /api/v1/journals/{id}", h.Get)
	mux.Handle("POST /api/v1/journals", idem(http.HandlerFunc(h.Create)))
	mux.Handle("PUT /api/v1/journals/{id}", idem(http.HandlerFunc(h.Update)))
	mux.HandleFunc("DELETE /api/v1/journals/{id}", h.Delete)
	ts := httptest.NewServer(v1.BearerOrCookie(db)(mux))
	t.Cleanup(ts.Close)
	return ts
}

func adminToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	var adminID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("admin user: %v", err)
	}
	_, plaintext, err := model.CreateAPIToken(db, adminID,
		fmt.Sprintf("test-%d", time.Now().UnixNano()),
		[]string{model.CapJournalsManage}, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return plaintext
}

func twoAccountIDs(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	rows, err := db.Query("SELECT id FROM accounts ORDER BY id LIMIT 2")
	if err != nil {
		t.Fatalf("accounts: %v", err)
	}
	defer rows.Close()
	var a, b int
	rows.Next()
	rows.Scan(&a)
	rows.Next()
	rows.Scan(&b)
	if a == 0 || b == 0 {
		t.Fatal("need 2 accounts")
	}
	return a, b
}

func doReq(t *testing.T, ts *httptest.Server, method, path, bearer, idemKey string, body any) *http.Response {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func regexpMatch(s, pattern string) bool {
	m, _ := regexp.MatchString(pattern, s)
	return m
}

func balancedBody(accA, accB int, amount string) map[string]any {
	return map[string]any{
		"entry_date":  "2026-05-10",
		"description": "Test entry",
		"lines": []map[string]any{
			{"account_id": accA, "debit": amount, "credit": "0", "memo": "Dr"},
			{"account_id": accB, "debit": "0", "credit": amount, "memo": "Cr"},
		},
	}
}

func TestListJournals(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)

	testutil.APIMatrix(t, ts, db, http.MethodGet, "/api/v1/journals", "", testutil.AuthMatrix{
		Anon:        http.StatusUnauthorized,
		ValidBearer: http.StatusOK,
	})

	token := adminToken(t, db)
	resp := doReq(t, ts, http.MethodGet, "/api/v1/journals", token, "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var env struct {
		Data []model.JournalEntry `json:"data"`
		Meta v1.Meta              `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Meta.PerPage <= 0 {
		t.Error("expected per_page > 0")
	}
}

func TestGetJournal(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminToken(t, db)
	a, b := twoAccountIDs(t, db)

	resp := doReq(t, ts, http.MethodPost, "/api/v1/journals", token, "", balancedBody(a, b, "100000"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var created struct {
		Data struct {
			ID    int `json:"id"`
			Lines []struct {
				Debit  string `json:"debit"`
				Credit string `json:"credit"`
			} `json:"lines"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	resp = doReq(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/journals/%d", created.Data.ID), token, "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	var got struct {
		Data struct {
			ID    int `json:"id"`
			Lines []struct {
				Debit  string `json:"debit"`
				Credit string `json:"credit"`
			} `json:"lines"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&got)
	if got.Data.ID != created.Data.ID {
		t.Errorf("id mismatch")
	}
	if len(got.Data.Lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(got.Data.Lines))
	}
	if got.Data.Lines[0].Debit != "100000" && got.Data.Lines[0].Credit != "100000" {
		t.Errorf("expected IDR string serialization, got debit=%q credit=%q",
			got.Data.Lines[0].Debit, got.Data.Lines[0].Credit)
	}

	resp2 := doReq(t, ts, http.MethodGet, "/api/v1/journals/999999", token, "", nil)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp2.StatusCode)
	}
}

func TestCreateJournal_Balanced(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminToken(t, db)
	a, b := twoAccountIDs(t, db)

	testutil.APIMatrix(t, ts, db, http.MethodPost, "/api/v1/journals",
		fmt.Sprintf(`{"entry_date":"2026-05-10","description":"Matrix","lines":[{"account_id":%d,"debit":"50000","credit":"0"},{"account_id":%d,"debit":"0","credit":"50000"}]}`, a, b),
		testutil.AuthMatrix{
			Anon:               http.StatusUnauthorized,
			ValidBearer:        http.StatusCreated,
			ScopeMissingBearer: http.StatusForbidden,
		})

	resp := doReq(t, ts, http.MethodPost, "/api/v1/journals", token, "", balancedBody(a, b, "100000"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var env struct {
		Data struct {
			ID        int    `json:"id"`
			Reference string `json:"reference"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&env)
	if env.Data.ID == 0 {
		t.Error("expected ID")
	}
	if !regexpMatch(env.Data.Reference, `^JE-\d{6}-\d{4}$`) {
		t.Errorf("reference %q does not match JE-YYYYMM-NNNN", env.Data.Reference)
	}
}

func TestCreateJournal_Unbalanced(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminToken(t, db)
	a, b := twoAccountIDs(t, db)

	body := map[string]any{
		"entry_date":  "2026-05-10",
		"description": "Unbalanced",
		"lines": []map[string]any{
			{"account_id": a, "debit": "100000", "credit": "0"},
			{"account_id": b, "debit": "0", "credit": "50000"},
		},
	}
	resp := doReq(t, ts, http.MethodPost, "/api/v1/journals", token, "", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}
	var ee v1.ErrorEnvelope
	json.NewDecoder(resp.Body).Decode(&ee)
	if ee.Code != v1.CodeValidationFailed {
		t.Errorf("expected validation_failed, got %q", ee.Code)
	}
	if ee.Fields["lines"] == "" {
		t.Errorf("expected lines field error, got %v", ee.Fields)
	}
}

func TestCreateJournal_Idempotency(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminToken(t, db)
	a, b := twoAccountIDs(t, db)

	key := "test-idem-" + fmt.Sprint(time.Now().UnixNano())

	r1 := doReq(t, ts, http.MethodPost, "/api/v1/journals", token, key, balancedBody(a, b, "75000"))
	if r1.StatusCode != http.StatusCreated {
		t.Fatalf("first create: %d", r1.StatusCode)
	}
	var b1 bytes.Buffer
	b1.ReadFrom(r1.Body)
	r1.Body.Close()

	r2 := doReq(t, ts, http.MethodPost, "/api/v1/journals", token, key, balancedBody(a, b, "75000"))
	if r2.StatusCode != http.StatusCreated {
		t.Fatalf("replay: %d", r2.StatusCode)
	}
	var b2 bytes.Buffer
	b2.ReadFrom(r2.Body)
	r2.Body.Close()

	if b1.String() != b2.String() {
		t.Errorf("expected identical responses; got %q vs %q", b1.String(), b2.String())
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE description = 'Test entry'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestUpdateJournal(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminToken(t, db)
	a, b := twoAccountIDs(t, db)

	resp := doReq(t, ts, http.MethodPost, "/api/v1/journals", token, "", balancedBody(a, b, "10000"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var created struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	updateBody := map[string]any{
		"entry_date":  "2026-05-11",
		"description": "Updated entry",
		"lines": []map[string]any{
			{"account_id": a, "debit": "20000", "credit": "0"},
			{"account_id": b, "debit": "0", "credit": "20000"},
		},
	}
	r2 := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/journals/%d", created.Data.ID), token, "", updateBody)
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("update: %d", r2.StatusCode)
	}

	var adminID int
	db.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&adminID)
	auto := &model.JournalEntry{
		EntryDate: "2026-05-09", Reference: "AUTO-1",
		Description: "Auto entry", SourceType: "income", IsPosted: true,
		CreatedBy: adminID,
	}
	autoID, err := model.CreateJournalEntry(db, auto, []model.JournalLine{
		{AccountID: a, Debit: 5000}, {AccountID: b, Credit: 5000},
	})
	if err != nil {
		t.Fatalf("create auto: %v", err)
	}
	r3 := doReq(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/journals/%d", autoID), token, "", updateBody)
	defer r3.Body.Close()
	if r3.StatusCode != http.StatusConflict {
		t.Errorf("expected 409 for auto-generated, got %d", r3.StatusCode)
	}
}

func TestDeleteJournal(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ts := newTestServer(t, db)
	token := adminToken(t, db)
	a, b := twoAccountIDs(t, db)

	resp := doReq(t, ts, http.MethodPost, "/api/v1/journals", token, "", balancedBody(a, b, "30000"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var created struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	testutil.APIMatrix(t, ts, db, http.MethodDelete,
		fmt.Sprintf("/api/v1/journals/%d", created.Data.ID), "",
		testutil.AuthMatrix{
			Anon:               http.StatusUnauthorized,
			ScopeMissingBearer: http.StatusForbidden,
		})

	r := doReq(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/journals/%d", created.Data.ID), token, "", nil)
	defer r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", r.StatusCode)
	}

	r2 := doReq(t, ts, http.MethodDelete, "/api/v1/journals/999999", token, "", nil)
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", r2.StatusCode)
	}
}
