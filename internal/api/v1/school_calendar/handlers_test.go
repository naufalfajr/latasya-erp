package school_calendar_test

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
	v1schoolcalendar "github.com/naufal/latasya-erp/internal/api/v1/school_calendar"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func setupServer(t *testing.T, config googlecalendar.Config) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := &v1schoolcalendar.Handler{DB: db, GoogleCalendarConfig: config}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("GET /api/v1/school-calendar/closures", h.ListClosures)
	apiMux.HandleFunc("POST /api/v1/school-calendar/closures", h.CreateClosure)
	apiMux.HandleFunc("DELETE /api/v1/school-calendar/closures/{id}", h.DeleteClosure)
	apiMux.HandleFunc("GET /api/v1/school-calendar/effective-days", h.EffectiveDays)
	apiMux.HandleFunc("POST /api/v1/integrations/google-calendar/sync", h.SyncGoogleCalendar)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/", v1.BearerOrCookie(db)(apiMux))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, db
}

func adminID(t *testing.T, db *sql.DB) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&id); err != nil {
		t.Fatalf("get admin: %v", err)
	}
	return id
}

func bearerFor(t *testing.T, db *sql.DB, userID int, scopes []string) string {
	t.Helper()
	_, plaintext, err := model.CreateAPIToken(db, userID, fmt.Sprintf("test-%d", time.Now().UnixNano()), scopes, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return plaintext
}

func doRequest(t *testing.T, ts *httptest.Server, method, path, token string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
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

func TestCreateClosurePermissionDenied(t *testing.T) {
	ts, db := setupServer(t, googlecalendar.Config{})
	token := bearerFor(t, db, adminID(t, db), nil)

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/school-calendar/closures", token, map[string]any{
		"title":      "Semester break",
		"start_date": "2026-06-01",
		"end_date":   "2026-06-05",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", resp.StatusCode)
	}
}

func TestListClosuresPermissionDenied(t *testing.T) {
	ts, db := setupServer(t, googlecalendar.Config{})
	token := bearerFor(t, db, adminID(t, db), nil)

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/school-calendar/closures?month=2026-06", token, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", resp.StatusCode)
	}
}

func TestManualClosureCreateListDelete(t *testing.T) {
	ts, db := setupServer(t, googlecalendar.Config{})
	token := bearerFor(t, db, adminID(t, db), []string{model.CapInvoicesManage})

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/school-calendar/closures", token, map[string]any{
		"title":      "Semester break",
		"start_date": "2026-06-01",
		"end_date":   "2026-06-05",
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status: got %d want 201", resp.StatusCode)
	}
	var created struct {
		Data model.SchoolClosure `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Data.Source != model.SchoolClosureSourceManual || created.Data.Title != "Semester break" {
		t.Fatalf("created closure mismatch: %+v", created.Data)
	}

	listResp := doRequest(t, ts, http.MethodGet, "/api/v1/school-calendar/closures?month=2026-06", token, nil)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status: got %d want 200", listResp.StatusCode)
	}
	var listed struct {
		Data []model.SchoolClosure `json:"data"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].ID != created.Data.ID {
		t.Fatalf("listed closures mismatch: %+v", listed.Data)
	}

	deleteResp := doRequest(t, ts, http.MethodDelete, fmt.Sprintf("/api/v1/school-calendar/closures/%d", created.Data.ID), token, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete status: got %d want 200", deleteResp.StatusCode)
	}
	closures, err := model.ListSchoolClosures(db, "2026-06")
	if err != nil {
		t.Fatalf("list model closures: %v", err)
	}
	if len(closures) != 0 {
		t.Fatalf("closures after delete: %+v", closures)
	}
}

func TestEffectiveDaysResponse(t *testing.T) {
	ts, db := setupServer(t, googlecalendar.Config{})
	token := bearerFor(t, db, adminID(t, db), []string{model.CapInvoicesManage})
	_, err := model.CreateSchoolClosure(db, &model.SchoolClosure{
		Source:    model.SchoolClosureSourceManual,
		Title:     "Long break",
		StartDate: "2026-06-01",
		EndDate:   "2026-06-15",
	})
	if err != nil {
		t.Fatalf("seed closure: %v", err)
	}

	resp := doRequest(t, ts, http.MethodGet, "/api/v1/school-calendar/effective-days?month=2026-06", token, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", resp.StatusCode)
	}
	var env struct {
		Data struct {
			Month             string `json:"month"`
			EffectiveDays     int    `json:"effective_days"`
			MultiplierPercent int    `json:"multiplier_percent"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Month != "2026-06" || env.Data.EffectiveDays != 13 || env.Data.MultiplierPercent != 75 {
		t.Fatalf("effective-days mismatch: %+v", env.Data)
	}
}

func TestGoogleCalendarSyncRequiresAdmin(t *testing.T) {
	ts, db := setupServer(t, googlecalendar.Config{})
	roleName := fmt.Sprintf("calendar-manager-%d", time.Now().UnixNano())
	if err := model.CreateRole(db, &model.Role{Name: roleName, Capabilities: []string{model.CapInvoicesManage}}); err != nil {
		t.Fatalf("create role: %v", err)
	}
	userID := testutil.CreateTestUser(t, db, "calendar-manager", "pw", roleName)
	token := bearerFor(t, db, userID, []string{model.CapInvoicesManage})

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/integrations/google-calendar/sync", token, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", resp.StatusCode)
	}
}

func TestGoogleCalendarSyncAdminWithScopeReachesConnectionCheck(t *testing.T) {
	ts, db := setupServer(t, googlecalendar.Config{})
	token := bearerFor(t, db, adminID(t, db), []string{model.CapInvoicesManage})

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/integrations/google-calendar/sync", token, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", resp.StatusCode)
	}
	var env struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error != "google calendar is not connected" {
		t.Fatalf("error: got %q", env.Error)
	}
}

func TestGoogleCalendarSyncAdminTokenStillNeedsScope(t *testing.T) {
	ts, db := setupServer(t, googlecalendar.Config{})
	token := bearerFor(t, db, adminID(t, db), nil)

	resp := doRequest(t, ts, http.MethodPost, "/api/v1/integrations/google-calendar/sync", token, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", resp.StatusCode)
	}
}
