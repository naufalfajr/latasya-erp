package contacts_test

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
	v1contacts "github.com/naufal/latasya-erp/internal/api/v1/contacts"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func setupServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)

	apiMux := http.NewServeMux()
	h := &v1contacts.Handler{DB: db}
	apiMux.HandleFunc("GET /api/v1/contacts", h.List)
	apiMux.HandleFunc("GET /api/v1/contacts/{id}", h.Get)
	apiMux.HandleFunc("POST /api/v1/contacts", h.Create)
	apiMux.HandleFunc("PUT /api/v1/contacts/{id}", h.Update)
	apiMux.HandleFunc("DELETE /api/v1/contacts/{id}", h.Delete)

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
		[]string{model.CapContactsManage}, nil)
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

func seedContact(t *testing.T, db *sql.DB, name, contactType string) int {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO contacts (name, contact_type, is_active) VALUES (?, ?, 1)",
		name, contactType,
	)
	if err != nil {
		t.Fatalf("seed contact: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
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

func TestListContacts(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	seedContact(t, db, "Acme Corp", "customer")
	seedContact(t, db, "Best Supplies", "supplier")

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/contacts", token, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var env struct {
		Data []model.Contact `json:"data"`
		Meta v1.Meta         `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Meta.Total < 2 {
		t.Errorf("total: got %d, want >= 2", env.Meta.Total)
	}
	if env.Meta.Page != 1 {
		t.Errorf("page: got %d, want 1", env.Meta.Page)
	}
}

func TestListContactsFilterByType(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)

	seedContact(t, db, "Customer One", "customer")
	seedContact(t, db, "Supplier One", "supplier")

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/contacts?type=customer", token, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var env struct {
		Data []model.Contact `json:"data"`
		Meta v1.Meta         `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, c := range env.Data {
		if c.ContactType != "customer" && c.ContactType != "both" {
			t.Errorf("expected customer/both, got %q", c.ContactType)
		}
	}
}

func TestGetContact(t *testing.T) {
	ts, db := setupServer(t)
	token := adminToken(t, db)
	id := seedContact(t, db, "Test Corp", "customer")

	t.Run("found", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, fmt.Sprintf("/api/v1/contacts/%d", id), token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d, want 200", resp.StatusCode)
		}
		var c model.Contact
		if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if c.ID != id {
			t.Errorf("id: got %d, want %d", c.ID, id)
		}
		if c.Name != "Test Corp" {
			t.Errorf("name: got %q, want %q", c.Name, "Test Corp")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		resp := doRequest(t, ts, http.MethodGet, "/api/v1/contacts/999999", token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})
}

func TestCreateContact(t *testing.T) {
	ts, db := setupServer(t)

	t.Run("success", func(t *testing.T) {
		token := adminToken(t, db)
		body := map[string]any{
			"name":         "New Customer",
			"contact_type": "customer",
			"phone":        "081234567890",
			"email":        "new@example.com",
		}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/contacts", token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status: got %d, want 201", resp.StatusCode)
		}
		var c model.Contact
		if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if c.Name != "New Customer" {
			t.Errorf("name: got %q, want %q", c.Name, "New Customer")
		}
		if c.ID == 0 {
			t.Errorf("id: got 0, want non-zero")
		}
	})

	t.Run("missing_name_422", func(t *testing.T) {
		token := adminToken(t, db)
		body := map[string]any{"contact_type": "supplier"}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/contacts", token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status: got %d, want 422", resp.StatusCode)
		}
		var env v1.ErrorEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Fields["name"] == "" {
			t.Errorf("expected field error for name, got none")
		}
	})

	t.Run("no_capability_403", func(t *testing.T) {
		token := noScopeToken(t, db)
		body := map[string]any{"name": "X", "contact_type": "customer"}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/contacts", token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", resp.StatusCode)
		}
	})

	t.Run("anon_401", func(t *testing.T) {
		body := map[string]any{"name": "X", "contact_type": "customer"}
		resp := doRequest(t, ts, http.MethodPost, "/api/v1/contacts", "", body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", resp.StatusCode)
		}
	})
}

func TestUpdateContact(t *testing.T) {
	ts, db := setupServer(t)
	id := seedContact(t, db, "Old Name", "customer")

	t.Run("success", func(t *testing.T) {
		token := adminToken(t, db)
		body := map[string]any{
			"name":         "New Name",
			"contact_type": "supplier",
		}
		resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/contacts/%d", id), token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: got %d, want 200", resp.StatusCode)
		}
		var c model.Contact
		if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if c.Name != "New Name" {
			t.Errorf("name: got %q, want %q", c.Name, "New Name")
		}
	})

	t.Run("no_capability_403", func(t *testing.T) {
		token := noScopeToken(t, db)
		body := map[string]any{"name": "X", "contact_type": "customer"}
		resp := doRequest(t, ts, http.MethodPut, fmt.Sprintf("/api/v1/contacts/%d", id), token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", resp.StatusCode)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		token := adminToken(t, db)
		body := map[string]any{"name": "X", "contact_type": "customer"}
		resp := doRequest(t, ts, http.MethodPut, "/api/v1/contacts/999999", token, body)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status: got %d, want 404", resp.StatusCode)
		}
	})
}

func TestDeleteContact(t *testing.T) {
	ts, db := setupServer(t)

	t.Run("success", func(t *testing.T) {
		token := adminToken(t, db)
		id := seedContact(t, db, "To Delete", "customer")

		resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/contacts/%d", id), token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status: got %d, want 204", resp.StatusCode)
		}
	})

	t.Run("no_capability_403", func(t *testing.T) {
		token := noScopeToken(t, db)
		id := seedContact(t, db, "Protected", "customer")

		resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/contacts/%d", id), token, nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status: got %d, want 403", resp.StatusCode)
		}
	})

	t.Run("anon_401", func(t *testing.T) {
		id := seedContact(t, db, "Anon Target", "customer")

		resp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/contacts/%d", id), "", nil)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status: got %d, want 401", resp.StatusCode)
		}
	})
}
