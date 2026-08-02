package googlecalendar

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	latasyaerp "github.com/naufal/latasya-erp"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/schoolcalendar"
)

// setupTestDB creates an in-memory SQLite database with migrations applied.
// This duplicates internal/testutil.SetupTestDB in miniature: importing
// testutil here would create an import cycle, since testutil -> handler ->
// googlecalendar.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database.SetMigrations(latasyaerp.MigrationFS)
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func saveGoogleCalendarConnection(db *sql.DB, connection *model.GoogleCalendarConnection) error {
	module := schoolcalendar.New(db)
	actor := schoolcalendar.Actor{UserID: 1, IsAdmin: true}
	ctx := context.Background()
	if _, err := module.SaveCalendarID(ctx, actor, connection.CalendarID); err != nil {
		return err
	}
	if connection.RefreshToken != "" && connection.IsActive {
		if _, err := module.Connect(ctx, actor, connection.RefreshToken); err != nil {
			return err
		}
	}
	if connection.LastSyncStatus != "" || connection.LastSyncError != "" {
		return module.UpdateSyncStatus(ctx, connection.LastSyncStatus, connection.LastSyncError)
	}
	return nil
}

func getGoogleCalendarConnection(db *sql.DB) (*model.GoogleCalendarConnection, error) {
	return schoolcalendar.New(db).Connection(context.Background())
}

func TestConvertEventAllDayExclusiveEnd(t *testing.T) {
	loc := time.FixedZone("Asia/Jakarta", 7*60*60)
	closure, ok := convertEvent(googleEvent{
		ID:      "event-1",
		Summary: "Semester break",
		Start:   googleEventTime{Date: "2026-06-10"},
		End:     googleEventTime{Date: "2026-06-13"},
	}, loc)
	if !ok {
		t.Fatal("event was skipped")
	}
	if closure.StartDate != "2026-06-10" || closure.EndDate != "2026-06-12" {
		t.Fatalf("closure dates: got %s..%s want 2026-06-10..2026-06-12", closure.StartDate, closure.EndDate)
	}
}

func TestConvertEventTimedMidnightEnd(t *testing.T) {
	loc := time.FixedZone("Asia/Jakarta", 7*60*60)
	closure, ok := convertEvent(googleEvent{
		ID:      "event-2",
		Summary: "Overnight break",
		Start:   googleEventTime{DateTime: "2026-06-10T08:00:00+07:00"},
		End:     googleEventTime{DateTime: "2026-06-12T00:00:00+07:00"},
	}, loc)
	if !ok {
		t.Fatal("event was skipped")
	}
	if closure.StartDate != "2026-06-10" || closure.EndDate != "2026-06-11" {
		t.Fatalf("closure dates: got %s..%s want 2026-06-10..2026-06-11", closure.StartDate, closure.EndDate)
	}
}

func TestConvertEventSkipsCancelledAndUnusableEvents(t *testing.T) {
	loc := time.FixedZone("Asia/Jakarta", 7*60*60)
	for _, event := range []googleEvent{
		{ID: "cancelled", Status: "cancelled", Summary: "Nope", Start: googleEventTime{Date: "2026-06-10"}, End: googleEventTime{Date: "2026-06-11"}},
		{ID: "missing-end", Summary: "Nope", Start: googleEventTime{Date: "2026-06-10"}},
		{ID: "blank-title", Summary: " ", Start: googleEventTime{Date: "2026-06-10"}, End: googleEventTime{Date: "2026-06-11"}},
	} {
		if _, ok := convertEvent(event, loc); ok {
			t.Fatalf("event should have been skipped: %+v", event)
		}
	}
}

func TestFetchEventsPagination(t *testing.T) {
	var pageTokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendar-id/events" {
			t.Fatalf("path: got %q want /calendar-id/events", r.URL.Path)
		}
		query := r.URL.Query()
		for key, want := range map[string]string{
			"singleEvents": "true",
			"showDeleted":  "false",
			"orderBy":      "startTime",
			"maxResults":   "250",
		} {
			if got := query.Get(key); got != want {
				t.Fatalf("%s: got %q want %q", key, got, want)
			}
		}
		pageTokens = append(pageTokens, query.Get("pageToken"))
		w.Header().Set("Content-Type", "application/json")
		if query.Get("pageToken") == "" {
			json.NewEncoder(w).Encode(eventsResponse{Items: []googleEvent{{ID: "one"}}, NextPageToken: "next"})
			return
		}
		json.NewEncoder(w).Encode(eventsResponse{Items: []googleEvent{{ID: "two"}}})
	}))
	defer server.Close()

	oldBaseURL := eventsBaseURL
	eventsBaseURL = server.URL
	t.Cleanup(func() { eventsBaseURL = oldBaseURL })

	events, err := fetchEvents(context.Background(), server.Client(), "calendar-id", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("fetch events: %v", err)
	}
	if len(events) != 2 || events[0].ID != "one" || events[1].ID != "two" {
		t.Fatalf("events = %+v", events)
	}
	if len(pageTokens) != 2 || pageTokens[0] != "" || pageTokens[1] != "next" {
		t.Fatalf("page tokens = %+v", pageTokens)
	}
}

func TestFetchEventsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	oldBaseURL := eventsBaseURL
	eventsBaseURL = server.URL
	t.Cleanup(func() { eventsBaseURL = oldBaseURL })

	_, err := fetchEvents(context.Background(), server.Client(), url.PathEscape("calendar-id"), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected fetch error")
	}
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"all set", Config{ClientID: "id", ClientSecret: "secret", RedirectURL: "https://app.example.com/cb"}, true},
		{"empty", Config{}, false},
		{"missing client id", Config{ClientSecret: "secret", RedirectURL: "https://app.example.com/cb"}, false},
		{"missing client secret", Config{ClientID: "id", RedirectURL: "https://app.example.com/cb"}, false},
		{"missing redirect url", Config{ClientID: "id", ClientSecret: "secret"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v want %v", got, tt.want)
			}
		})
	}
}

func TestGeneratePKCEVerifier(t *testing.T) {
	v1 := GeneratePKCEVerifier()
	v2 := GeneratePKCEVerifier()
	if v1 == v2 {
		t.Fatal("expected distinct verifiers across calls")
	}
	const allowed = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	for _, v := range []string{v1, v2} {
		if len(v) < 43 || len(v) > 128 {
			t.Fatalf("verifier length %d out of RFC 7636 range [43,128]: %q", len(v), v)
		}
		for _, r := range v {
			if !strings.ContainsRune(allowed, r) {
				t.Fatalf("verifier contains disallowed character %q: %q", r, v)
			}
		}
	}
}

func TestOAuth2Config(t *testing.T) {
	c := Config{ClientID: "cid", ClientSecret: "csecret", RedirectURL: "https://app.example.com/cb"}
	oc := c.oauth2Config()
	if oc.ClientID != c.ClientID || oc.ClientSecret != c.ClientSecret || oc.RedirectURL != c.RedirectURL {
		t.Fatalf("oauth2Config fields = %+v", oc)
	}
	if len(oc.Scopes) != 1 || oc.Scopes[0] != calendarReadonlyScope {
		t.Fatalf("oauth2Config scopes = %+v", oc.Scopes)
	}
	if oc.Endpoint != oauthEndpoint {
		t.Fatalf("oauth2Config endpoint = %+v want %+v", oc.Endpoint, oauthEndpoint)
	}
}

func TestOAuthURL(t *testing.T) {
	c := Config{ClientID: "client-123", ClientSecret: "secret", RedirectURL: "https://app.example.com/callback"}
	verifier := "test-pkce-verifier-1234567890123456789012345"
	got := c.OAuthURL("state-xyz", verifier)

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse oauth url: %v", err)
	}
	if u.Scheme+"://"+u.Host+u.Path != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Fatalf("auth endpoint: got %s", got)
	}
	q := u.Query()
	want := map[string]string{
		"client_id":             c.ClientID,
		"redirect_uri":          c.RedirectURL,
		"response_type":         "code",
		"scope":                 calendarReadonlyScope,
		"access_type":           "offline",
		"prompt":                "consent",
		"state":                 "state-xyz",
		"code_challenge_method": "S256",
	}
	for key, wantVal := range want {
		if got := q.Get(key); got != wantVal {
			t.Errorf("%s: got %q want %q", key, got, wantVal)
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := q.Get("code_challenge"); got != wantChallenge {
		t.Errorf("code_challenge: got %q want %q", got, wantChallenge)
	}
}

func TestExchangeSuccess(t *testing.T) {
	var gotGrantType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotGrantType = r.PostForm.Get("grant_type")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"token_type":    "Bearer",
			"refresh_token": "test-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	oldEndpoint := oauthEndpoint
	oauthEndpoint.TokenURL = server.URL
	t.Cleanup(func() { oauthEndpoint = oldEndpoint })

	c := Config{ClientID: "cid", ClientSecret: "csecret", RedirectURL: "https://app.example.com/cb"}
	tok, err := c.Exchange(context.Background(), "auth-code", "verifier-value")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if tok.AccessToken != "test-access-token" {
		t.Fatalf("access token: got %q want test-access-token", tok.AccessToken)
	}
	if gotGrantType != "authorization_code" {
		t.Fatalf("grant_type: got %q want authorization_code", gotGrantType)
	}
}

func TestExchangeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid_grant", http.StatusBadRequest)
	}))
	defer server.Close()

	oldEndpoint := oauthEndpoint
	oauthEndpoint.TokenURL = server.URL
	t.Cleanup(func() { oauthEndpoint = oldEndpoint })

	c := Config{ClientID: "cid", ClientSecret: "csecret", RedirectURL: "https://app.example.com/cb"}
	_, err := c.Exchange(context.Background(), "auth-code", "verifier-value")
	if err == nil {
		t.Fatal("expected exchange error")
	}
	if !strings.Contains(err.Error(), "exchange google oauth code") {
		t.Fatalf("error message: got %q, want it to contain %q", err.Error(), "exchange google oauth code")
	}
}

func TestSyncNotConnected(t *testing.T) {
	db := setupTestDB(t)
	// Seed an existing connection row so UpdateGoogleCalendarSyncStatus (an
	// UPDATE ... WHERE id = 1) has a row to update; without one the update
	// is a no-op and LastSyncStatus would stay empty regardless of Sync's
	// error handling.
	if err := saveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{
		CalendarID:   "cal-primary",
		RefreshToken: "refresh-tok",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("save connection: %v", err)
	}

	// Config{} is not Enabled(), so Sync should fail before making any
	// network calls or touching fetchEvents.
	_, err := Sync(context.Background(), schoolcalendar.New(db), Config{}, "")
	if err == nil {
		t.Fatal("expected sync error when google calendar is not connected")
	}

	conn, err := getGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.LastSyncStatus != "error" || conn.LastSyncError == "" {
		t.Fatalf("connection sync status after failed sync = %+v", conn)
	}
}

func TestSyncSuccess(t *testing.T) {
	db := setupTestDB(t)
	if err := saveGoogleCalendarConnection(db, &model.GoogleCalendarConnection{
		CalendarID:   "cal-primary",
		RefreshToken: "refresh-tok",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("save connection: %v", err)
	}

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-tok",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenServer.Close()

	eventsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cal-primary/events" {
			t.Fatalf("events path: got %q want /cal-primary/events", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(eventsResponse{Items: []googleEvent{
			{ID: "evt-1", Summary: "Semester break", Start: googleEventTime{Date: "2026-06-10"}, End: googleEventTime{Date: "2026-06-13"}},
			{ID: "evt-2", Status: "cancelled", Summary: "Cancelled", Start: googleEventTime{Date: "2026-06-01"}, End: googleEventTime{Date: "2026-06-02"}},
		}})
	}))
	defer eventsServer.Close()

	oldEndpoint := oauthEndpoint
	oauthEndpoint.TokenURL = tokenServer.URL
	t.Cleanup(func() { oauthEndpoint = oldEndpoint })

	oldBaseURL := eventsBaseURL
	eventsBaseURL = eventsServer.URL
	t.Cleanup(func() { eventsBaseURL = oldBaseURL })

	cfg := Config{ClientID: "cid", ClientSecret: "csecret", RedirectURL: "https://app.example.com/cb"}
	result, err := Sync(context.Background(), schoolcalendar.New(db), cfg, "")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Fetched != 2 {
		t.Fatalf("fetched: got %d want 2", result.Fetched)
	}
	if result.Stored != 1 {
		t.Fatalf("stored: got %d want 1 (cancelled event filtered)", result.Stored)
	}

	conn, err := getGoogleCalendarConnection(db)
	if err != nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.LastSyncStatus != "success" {
		t.Fatalf("last sync status: got %q want success", conn.LastSyncStatus)
	}
}
