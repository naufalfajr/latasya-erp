package handler_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/handler"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func testServerWithSchoolCalendar(t *testing.T, config googlecalendar.Config) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)
	h.GoogleCalendarConfig = config

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /settings/school-calendar", auth.AdminOnly(h.SchoolCalendarPage))
	protected.HandleFunc("POST /settings/school-calendar/closures", auth.AdminOnly(h.CreateSchoolClosure))
	protected.HandleFunc("POST /settings/school-calendar/closures/{id}/delete", auth.AdminOnly(h.DeleteSchoolClosure))
	protected.HandleFunc("POST /settings/school-calendar/google-calendar-id", auth.AdminOnly(h.SaveGoogleCalendarID))
	protected.HandleFunc("POST /integrations/google-calendar/connect", auth.AdminOnly(h.ConnectGoogleCalendar))
	protected.HandleFunc("GET /integrations/google-calendar/callback", auth.AdminOnly(h.GoogleCalendarCallback))
	protected.HandleFunc("POST /integrations/google-calendar/sync", auth.AdminOnly(h.SyncGoogleCalendar))
	protected.HandleFunc("POST /integrations/google-calendar/disconnect", auth.AdminOnly(h.DisconnectGoogleCalendar))
	protected.HandleFunc("GET /password/change", h.PasswordChangePage)
	protected.HandleFunc("POST /password/change", h.PasswordChange)

	mux.Handle("/", auth.RequireAuth(db, auth.CSRFProtect(handler.EnforcePasswordChange(protected))))

	hash, err := auth.HashPassword(adminTestPassword)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	if _, err := db.Exec("UPDATE users SET password=?, must_change_password=0 WHERE username='admin'", hash); err != nil {
		t.Fatalf("update admin: %v", err)
	}

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, db
}

func TestSchoolCalendarPage_AdminRenders(t *testing.T) {
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/settings/school-calendar?month=2026-06", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyText := string(body)
	for _, want := range []string{"School Calendar", "Effective School Days", "Google Calendar", "Google OAuth is not configured"} {
		if !strings.Contains(bodyText, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(bodyText, "refresh-token") {
		t.Error("page rendered a refresh token")
	}
}

func TestSchoolCalendarPage_ViewerForbidden(t *testing.T) {
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsViewer(t, ts, db)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/settings/school-calendar", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer, got %d", resp.StatusCode)
	}
}

func TestCreateSchoolClosure_PersistsAndRedirects(t *testing.T) {
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"title":      {"Semester break"},
		"start_date": {"2026-06-10"},
		"end_date":   {"2026-06-12"},
	}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/closures?month=2026-06", cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/settings/school-calendar?month=2026-06" {
		t.Fatalf("Location = %q", got)
	}

	closures, err := model.ListSchoolClosures(db, "2026-06")
	if err != nil {
		t.Fatalf("list closures: %v", err)
	}
	if len(closures) != 1 || closures[0].Title != "Semester break" || closures[0].Source != model.SchoolClosureSourceManual {
		t.Fatalf("closures = %+v", closures)
	}
}

func TestSaveGoogleCalendarID_PreservesRefreshToken(t *testing.T) {
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	if err := model.SaveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{
		CalendarID:     "old-calendar",
		RefreshToken:   "refresh-token",
		IsActive:       true,
		LastSyncStatus: "success",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	form := url.Values{"calendar_id": {"school-calendar@example.com"}}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/google-calendar-id?month=2026-06", cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	conn, err := model.GetGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.CalendarID != "school-calendar@example.com" {
		t.Fatalf("CalendarID = %q", conn.CalendarID)
	}
	if conn.RefreshToken != "refresh-token" {
		t.Fatalf("RefreshToken changed to %q", conn.RefreshToken)
	}
	if !conn.IsActive || conn.LastSyncStatus != "success" {
		t.Fatalf("connection flags changed: %+v", conn)
	}
}

func TestGoogleCalendarConnect_MissingConfigDisabledAndRedirects(t *testing.T) {
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/settings/school-calendar", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("page request: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Google OAuth is not configured") || !strings.Contains(string(body), "Connect Google") || !strings.Contains(string(body), "disabled") {
		t.Fatalf("missing config state not rendered")
	}

	req, _ = requestWithCookies(db, "POST", ts.URL+"/integrations/google-calendar/connect?month=2026-06", cookies, "")
	resp, err = noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("connect request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/settings/school-calendar?month=2026-06" {
		t.Fatalf("Location = %q", got)
	}
}
