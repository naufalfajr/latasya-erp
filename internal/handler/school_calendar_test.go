package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// testServerWithSchoolCalendar wires up the school-calendar and Google
// Calendar integration routes. When mockHTTPClient is provided, it is
// injected into every incoming request's context under the oauth2.HTTPClient
// key, which the golang.org/x/oauth2 library (used by internal/googlecalendar)
// consults for its outbound HTTP calls. This lets tests fake Google's OAuth
// token and Calendar Events responses entirely in-process, without any
// production code changes and without ever touching the real network.
func testServerWithSchoolCalendar(t *testing.T, config googlecalendar.Config, mockHTTPClient ...*http.Client) (*httptest.Server, *sql.DB) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	h := testutil.SetupTestHandler(t, db)
	h.GoogleCalendarConfig = config

	mux := http.NewServeMux()
	h.RegisterAuthRoutes(mux, func(next http.Handler) http.Handler { return next })

	protected := http.NewServeMux()
	h.RegisterAccessRoutes(protected)
	h.RegisterSettingsRoutes(protected)

	mux.Handle("/", auth.RequireAuth(db, access.New(db, nil), auth.CSRFProtect(h.EnforcePasswordChange(protected))))

	hash, err := auth.HashPassword(adminTestPassword)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	if _, err := db.Exec("UPDATE users SET password=?, must_change_password=0 WHERE username='admin'", hash); err != nil {
		t.Fatalf("update admin: %v", err)
	}

	ts := httptest.NewUnstartedServer(mux)
	if len(mockHTTPClient) > 0 && mockHTTPClient[0] != nil {
		client := mockHTTPClient[0]
		ts.Config.BaseContext = func(net.Listener) context.Context {
			return context.WithValue(context.Background(), oauth2.HTTPClient, client)
		}
	}
	ts.Start()
	t.Cleanup(ts.Close)
	return ts, db
}

// roundTripFunc adapts a plain function to http.RoundTripper so tests can
// fake HTTP responses in-process (no real network access).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonRoundTrip(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": {"application/json"}},
	}
}

// enabledConfig returns a googlecalendar.Config with all fields populated so
// GoogleCalendarConfig.Enabled() reports true.
func enabledConfig() googlecalendar.Config {
	return googlecalendar.Config{ClientID: "test-client-id", ClientSecret: "test-secret", RedirectURL: "http://example.com/oauth/callback"}
}

func adminUserID(t *testing.T, db *sql.DB) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&id); err != nil {
		t.Fatalf("get admin user id: %v", err)
	}
	return id
}

func seedOAuthState(t *testing.T, db *sql.DB, userID int, state, verifier string) {
	t.Helper()
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	if err := testutil.CreateGoogleOAuthState(db, state, userID, verifier, expiresAt); err != nil {
		t.Fatalf("seed oauth state: %v", err)
	}
}

func flashCookieValue(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == "flash" {
			return c.Value
		}
	}
	return ""
}

func TestSchoolCalendarPage_AdminRenders(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

	closures, err := testutil.ListSchoolClosures(db, "2026-06")
	if err != nil {
		t.Fatalf("list closures: %v", err)
	}
	if len(closures) != 1 || closures[0].Title != "Semester break" || closures[0].Source != model.SchoolClosureSourceManual {
		t.Fatalf("closures = %+v", closures)
	}
}

func TestSaveGoogleCalendarID_PreservesRefreshToken(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	if err := testutil.SaveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{
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
	conn, err := testutil.GetGoogleCalendarConnection(db)
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
	t.Parallel()
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

// --- SchoolCalendarPage -----------------------------------------------------

func TestSchoolCalendarPage_LoadErrorRendersFallback(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	if _, err := db.Exec("DROP TABLE google_calendar_connections"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/settings/school-calendar?month=2026-06", cookies, "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (fallback render), got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Failed to load school calendar settings") {
		t.Fatalf("expected fallback error message in body, got: %s", body)
	}
}

// --- CreateSchoolClosure -----------------------------------------------------

func TestCreateSchoolClosure_ValidationErrorsRerenderForm(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"title":      {""},
		"start_date": {"2026-06-12"},
		"end_date":   {"2026-06-10"},
	}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/closures?month=2026-06", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (re-render with errors), got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyText := string(body)
	for _, want := range []string{"Title is required", "End date must be on or after start date"} {
		if !strings.Contains(bodyText, want) {
			t.Errorf("body missing %q", want)
		}
	}

	closures, err := testutil.ListSchoolClosures(db, "2026-06")
	if err != nil {
		t.Fatalf("list closures: %v", err)
	}
	if len(closures) != 0 {
		t.Fatalf("expected no closure created, got %+v", closures)
	}
}

func TestCreateSchoolClosure_InvalidFormReturns400(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/closures?month=2026-06", cookies, "title=%zz")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateSchoolClosure_DBErrorRendersFallback(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	if _, err := db.Exec("DROP TABLE school_closures"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	form := url.Values{
		"title":      {"Semester break"},
		"start_date": {"2026-06-10"},
		"end_date":   {"2026-06-12"},
	}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/closures?month=2026-06", cookies, form)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

// --- DeleteSchoolClosure -----------------------------------------------------

func TestDeleteSchoolClosure_RemovesRowAndRedirects(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	form := url.Values{
		"title":      {"Semester break"},
		"start_date": {"2026-06-10"},
		"end_date":   {"2026-06-12"},
	}.Encode()
	createReq, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/closures?month=2026-06", cookies, form)
	createResp, err := noRedirectClient().Do(createReq)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	createResp.Body.Close()

	closures, err := testutil.ListSchoolClosures(db, "2026-06")
	if err != nil || len(closures) != 1 {
		t.Fatalf("expected one seeded closure, got %+v err=%v", closures, err)
	}
	id := closures[0].ID

	delReq, _ := requestWithCookies(db, "POST", fmt.Sprintf("%s/settings/school-calendar/closures/%d/delete?month=2026-06", ts.URL, id), cookies, "")
	delResp, err := noRedirectClient().Do(delReq)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer delResp.Body.Close()

	if delResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", delResp.StatusCode)
	}
	if got := delResp.Header.Get("Location"); got != "/settings/school-calendar?month=2026-06" {
		t.Fatalf("Location = %q", got)
	}
	if flash := flashCookieValue(delResp); flash != "School closure deleted" {
		t.Fatalf("flash = %q", flash)
	}

	remaining, err := testutil.ListSchoolClosures(db, "2026-06")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected closure removed, got %+v", remaining)
	}
}

func TestDeleteSchoolClosure_UnknownIDStillRedirects(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/closures/999999/delete?month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "School closure deleted" {
		t.Fatalf("flash = %q", flash)
	}
}

func TestDeleteSchoolClosure_InvalidIDReturns404(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/closures/abc/delete?month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// --- SaveGoogleCalendarID ----------------------------------------------------

func TestSaveGoogleCalendarID_InvalidFormReturns400(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/google-calendar-id?month=2026-06", cookies, "calendar_id=%zz")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestSaveGoogleCalendarID_LoadErrorSetsFlash(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	if _, err := db.Exec("DROP TABLE google_calendar_connections"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	form := url.Values{"calendar_id": {"school-calendar@example.com"}}.Encode()
	req, _ := requestWithCookies(db, "POST", ts.URL+"/settings/school-calendar/google-calendar-id?month=2026-06", cookies, form)
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	flash := flashCookieValue(resp)
	if !strings.HasPrefix(flash, "Error loading Google Calendar settings: ") {
		t.Fatalf("flash = %q", flash)
	}
}

// --- ConnectGoogleCalendar ----------------------------------------------------

func TestConnectGoogleCalendar_EnabledRedirectsWithPersistedState(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, enabledConfig())
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/integrations/google-calendar/connect?month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if u.Host != "accounts.google.com" || !strings.HasPrefix(u.Path, "/o/oauth2/v2/auth") {
		t.Fatalf("unexpected redirect target: %s", loc)
	}
	if got := u.Query().Get("client_id"); got != "test-client-id" {
		t.Fatalf("client_id = %q", got)
	}
	if u.Query().Get("code_challenge") == "" {
		t.Fatal("expected PKCE code_challenge param")
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("expected state query param")
	}

	adminID := adminUserID(t, db)
	oauthState, err := testutil.ConsumeGoogleOAuthState(db, state, adminID)
	if err != nil {
		t.Fatalf("expected persisted oauth state, got err: %v", err)
	}
	if oauthState.PKCEVerifier == "" {
		t.Fatal("expected PKCE verifier stored alongside state")
	}
}

func TestConnectGoogleCalendar_StateCreateErrorSetsFlash(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, enabledConfig())
	cookies := loginAsAdmin(t, ts)

	if _, err := db.Exec("DROP TABLE google_oauth_states"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	req, _ := requestWithCookies(db, "POST", ts.URL+"/integrations/google-calendar/connect?month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Error starting Google connection" {
		t.Fatalf("flash = %q", flash)
	}
}

// --- GoogleCalendarCallback ---------------------------------------------------
//
// GoogleCalendarCallback's token exchange goes through golang.org/x/oauth2,
// which honors an *http.Client injected into the request context via the
// oauth2.HTTPClient key (see testServerWithSchoolCalendar). That lets these
// tests exercise the real success path with a fully in-process fake
// transport instead of hitting accounts.google.com. The one branch left
// untested is randomOAuthState's crypto/rand failure path: rand.Reader isn't
// swappable from outside the package, so it can't be forced to fail without
// a production DI change.

func TestGoogleCalendarCallback_DisabledConfigRedirects(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?code=abc&state=xyz&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Google Calendar OAuth is not configured" {
		t.Fatalf("flash = %q", flash)
	}
}

func TestGoogleCalendarCallback_ErrorParamCancelled(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, enabledConfig())
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?error=access_denied&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Google Calendar connection cancelled" {
		t.Fatalf("flash = %q", flash)
	}
}

func TestGoogleCalendarCallback_MissingCodeOrState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		query string
	}{
		{"missing code", "state=xyz"},
		{"missing state", "code=abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, db := testServerWithSchoolCalendar(t, enabledConfig())
			cookies := loginAsAdmin(t, ts)

			req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?"+tc.query+"&month=2026-06", cookies, "")
			resp, err := noRedirectClient().Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected 303, got %d", resp.StatusCode)
			}
			if flash := flashCookieValue(resp); flash != "Google Calendar callback was missing required values" {
				t.Fatalf("flash = %q", flash)
			}
		})
	}
}

func TestGoogleCalendarCallback_UnknownStateExpiredMessage(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, enabledConfig())
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?code=abc&state=does-not-exist&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Google Calendar connection expired. Please try again." {
		t.Fatalf("flash = %q", flash)
	}
}

func TestGoogleCalendarCallback_StateConsumeErrorSetsFlash(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, enabledConfig())
	cookies := loginAsAdmin(t, ts)
	adminID := adminUserID(t, db)
	seedOAuthState(t, db, adminID, "state-consume-err", "verifier-consume-err")

	if _, err := db.Exec("DROP TABLE google_oauth_states"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?code=auth-code&state=state-consume-err&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Error validating Google connection" {
		t.Fatalf("flash = %q", flash)
	}
}

func TestGoogleCalendarCallback_ExchangeFailureSetsFlash(t *testing.T) {
	t.Parallel()
	mockClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonRoundTrip(400, `{"error":"invalid_grant"}`), nil
	})}
	ts, db := testServerWithSchoolCalendar(t, enabledConfig(), mockClient)
	cookies := loginAsAdmin(t, ts)
	adminID := adminUserID(t, db)
	seedOAuthState(t, db, adminID, "state-exchange-fail", "verifier-exchange-fail")

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?code=auth-code&state=state-exchange-fail&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Google Calendar authorization failed" {
		t.Fatalf("flash = %q", flash)
	}
}

func TestGoogleCalendarCallback_SuccessSavesConnection(t *testing.T) {
	t.Parallel()
	mockClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonRoundTrip(200, `{"access_token":"fake-access","refresh_token":"fake-refresh","token_type":"Bearer","expires_in":3600}`), nil
	})}
	ts, db := testServerWithSchoolCalendar(t, enabledConfig(), mockClient)
	cookies := loginAsAdmin(t, ts)
	adminID := adminUserID(t, db)
	seedOAuthState(t, db, adminID, "state-success", "verifier-success")

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?code=auth-code&state=state-success&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Google Calendar connected" {
		t.Fatalf("flash = %q", flash)
	}

	conn, err := testutil.GetGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.RefreshToken != "fake-refresh" || !conn.IsActive {
		t.Fatalf("connection not saved as expected: %+v", conn)
	}

	if _, err := testutil.ConsumeGoogleOAuthState(db, "state-success", adminID); err == nil {
		t.Fatal("expected oauth state to be single-use (already consumed)")
	}
}

func TestGoogleCalendarCallback_MissingRefreshTokenFallsBackToExisting(t *testing.T) {
	t.Parallel()
	mockClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonRoundTrip(200, `{"access_token":"fake-access","token_type":"Bearer","expires_in":3600}`), nil
	})}
	ts, db := testServerWithSchoolCalendar(t, enabledConfig(), mockClient)
	cookies := loginAsAdmin(t, ts)

	if err := testutil.SaveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{
		CalendarID:   "existing-cal",
		RefreshToken: "existing-refresh",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	adminID := adminUserID(t, db)
	seedOAuthState(t, db, adminID, "state-fallback", "verifier-fallback")

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?code=auth-code&state=state-fallback&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if flash := flashCookieValue(resp); flash != "Google Calendar connected" {
		t.Fatalf("flash = %q", flash)
	}
	conn, err := testutil.GetGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.RefreshToken != "existing-refresh" {
		t.Fatalf("expected fallback to existing refresh token, got %q", conn.RefreshToken)
	}
}

func TestGoogleCalendarCallback_NoRefreshTokenSetsFlash(t *testing.T) {
	t.Parallel()
	mockClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonRoundTrip(200, `{"access_token":"fake-access","token_type":"Bearer","expires_in":3600}`), nil
	})}
	ts, db := testServerWithSchoolCalendar(t, enabledConfig(), mockClient)
	cookies := loginAsAdmin(t, ts)
	adminID := adminUserID(t, db)
	seedOAuthState(t, db, adminID, "state-no-refresh", "verifier-no-refresh")

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?code=auth-code&state=state-no-refresh&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if flash := flashCookieValue(resp); flash != "Google did not return a refresh token. Please reconnect and approve offline access." {
		t.Fatalf("flash = %q", flash)
	}
	conn, err := testutil.GetGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.IsActive {
		t.Fatalf("connection should not be marked active without a refresh token: %+v", conn)
	}
}

func TestGoogleCalendarCallback_LoadConnectionErrorSetsFlash(t *testing.T) {
	t.Parallel()
	mockClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonRoundTrip(200, `{"access_token":"fake-access","refresh_token":"fake-refresh","token_type":"Bearer","expires_in":3600}`), nil
	})}
	ts, db := testServerWithSchoolCalendar(t, enabledConfig(), mockClient)
	cookies := loginAsAdmin(t, ts)
	adminID := adminUserID(t, db)
	seedOAuthState(t, db, adminID, "state-load-err", "verifier-load-err")

	if _, err := db.Exec("DROP TABLE google_calendar_connections"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	req, _ := requestWithCookies(db, "GET", ts.URL+"/integrations/google-calendar/callback?code=auth-code&state=state-load-err&month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if flash := flashCookieValue(resp); flash != "Error loading Google Calendar settings" {
		t.Fatalf("flash = %q", flash)
	}
}

// --- SyncGoogleCalendar -------------------------------------------------------

func TestSyncGoogleCalendar_NotConnectedSetsFlash(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	req, _ := requestWithCookies(db, "POST", ts.URL+"/integrations/google-calendar/sync?month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Google Calendar sync failed: google calendar is not connected" {
		t.Fatalf("flash = %q", flash)
	}
}

func TestSyncGoogleCalendar_SuccessStoresClosureAndSetsFlash(t *testing.T) {
	t.Parallel()
	eventDate := time.Now().UTC().Format("2006-01-02")
	nextDay := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	mockClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/events") {
			body := fmt.Sprintf(`{"items":[{"id":"evt-1","status":"confirmed","summary":"Holiday","start":{"date":"%s"},"end":{"date":"%s"}}],"nextPageToken":""}`, eventDate, nextDay)
			return jsonRoundTrip(200, body), nil
		}
		return jsonRoundTrip(200, `{"access_token":"fake-access","token_type":"Bearer","expires_in":3600}`), nil
	})}
	ts, db := testServerWithSchoolCalendar(t, enabledConfig(), mockClient)
	cookies := loginAsAdmin(t, ts)

	if err := testutil.SaveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{
		CalendarID:   "school@example.com",
		RefreshToken: "existing-refresh",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	req, _ := requestWithCookies(db, "POST", ts.URL+"/integrations/google-calendar/sync?month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Google Calendar synced: fetched 1 event(s), stored 1 closure(s)." {
		t.Fatalf("flash = %q", flash)
	}

	conn, err := testutil.GetGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.LastSyncStatus != "success" {
		t.Fatalf("LastSyncStatus = %q", conn.LastSyncStatus)
	}
}

// --- DisconnectGoogleCalendar --------------------------------------------------

func TestDisconnectGoogleCalendar_RemovesConnectionAndRedirects(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	if err := testutil.SaveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{
		CalendarID:   "school@example.com",
		RefreshToken: "refresh-token",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	req, _ := requestWithCookies(db, "POST", ts.URL+"/integrations/google-calendar/disconnect?month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	if flash := flashCookieValue(resp); flash != "Google Calendar disconnected" {
		t.Fatalf("flash = %q", flash)
	}

	conn, err := testutil.GetGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.IsActive || conn.RefreshToken != "" {
		t.Fatalf("expected connection cleared, got %+v", conn)
	}
}

func TestDisconnectGoogleCalendar_DBErrorSetsFlash(t *testing.T) {
	t.Parallel()
	ts, db := testServerWithSchoolCalendar(t, googlecalendar.Config{})
	cookies := loginAsAdmin(t, ts)

	if _, err := db.Exec("DROP TABLE google_calendar_connections"); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	req, _ := requestWithCookies(db, "POST", ts.URL+"/integrations/google-calendar/disconnect?month=2026-06", cookies, "")
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	flash := flashCookieValue(resp)
	if !strings.HasPrefix(flash, "Error disconnecting Google Calendar: ") {
		t.Fatalf("flash = %q", flash)
	}
}
