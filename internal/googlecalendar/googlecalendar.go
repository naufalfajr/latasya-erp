package googlecalendar

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
	"golang.org/x/oauth2"
)

const calendarReadonlyScope = "https://www.googleapis.com/auth/calendar.events.readonly"

var eventsBaseURL = "https://www.googleapis.com/calendar/v3/calendars"

var oauthEndpoint = oauth2.Endpoint{
	AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
}

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type SyncResult struct {
	Fetched     int    `json:"fetched"`
	Stored      int    `json:"stored"`
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
}

type googleEvent struct {
	ID      string          `json:"id"`
	Status  string          `json:"status"`
	Summary string          `json:"summary"`
	Start   googleEventTime `json:"start"`
	End     googleEventTime `json:"end"`
}

type googleEventTime struct {
	Date     string `json:"date"`
	DateTime string `json:"dateTime"`
}

type eventsResponse struct {
	Items         []googleEvent `json:"items"`
	NextPageToken string        `json:"nextPageToken"`
}

func (c Config) Enabled() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

func GeneratePKCEVerifier() string {
	return oauth2.GenerateVerifier()
}

func (c Config) OAuthURL(state, pkceVerifier string) string {
	return c.oauth2Config().AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
		oauth2.S256ChallengeOption(pkceVerifier),
	)
}

func (c Config) Exchange(ctx context.Context, code, pkceVerifier string) (*oauth2.Token, error) {
	tok, err := c.oauth2Config().Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return nil, fmt.Errorf("exchange google oauth code: %w", err)
	}
	return tok, nil
}

func Sync(ctx context.Context, db *sql.DB, config Config, calendarID string) (SyncResult, error) {
	var result SyncResult
	conn, err := model.GetGoogleCalendarConnection(db)
	if err != nil {
		return result, err
	}
	if calendarID == "" {
		calendarID = conn.CalendarID
	}
	if !config.Enabled() || !conn.IsActive || conn.RefreshToken == "" || calendarID == "" {
		err := fmt.Errorf("google calendar is not connected")
		_ = model.UpdateGoogleCalendarSyncStatus(db, "error", err.Error())
		return result, err
	}

	now := time.Now()
	windowStart := now.AddDate(-1, 0, 0)
	windowEnd := now.AddDate(1, 6, 0)
	result.WindowStart = windowStart.Format("2006-01-02")
	result.WindowEnd = windowEnd.Format("2006-01-02")

	httpClient := oauth2.NewClient(ctx, config.oauth2Config().TokenSource(ctx, &oauth2.Token{RefreshToken: conn.RefreshToken}))
	events, err := fetchEvents(ctx, httpClient, calendarID, windowStart, windowEnd)
	if err != nil {
		_ = model.UpdateGoogleCalendarSyncStatus(db, "error", err.Error())
		return result, err
	}
	result.Fetched = len(events)

	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		_ = model.UpdateGoogleCalendarSyncStatus(db, "error", err.Error())
		return result, fmt.Errorf("load Asia/Jakarta: %w", err)
	}
	closures := make([]model.SchoolClosure, 0, len(events))
	for _, event := range events {
		closure, ok := convertEvent(event, jakarta)
		if ok {
			closures = append(closures, closure)
		}
	}

	if err := model.ReplaceGoogleSchoolClosures(db, closures, result.WindowStart, result.WindowEnd); err != nil {
		_ = model.UpdateGoogleCalendarSyncStatus(db, "error", err.Error())
		return result, err
	}
	if err := model.UpdateGoogleCalendarSyncStatus(db, "success", ""); err != nil {
		return result, err
	}
	result.Stored = len(closures)
	return result, nil
}

func (c Config) oauth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURL,
		Scopes:       []string{calendarReadonlyScope},
		Endpoint:     oauthEndpoint,
	}
}

func fetchEvents(ctx context.Context, client *http.Client, calendarID string, windowStart, windowEnd time.Time) ([]googleEvent, error) {
	var events []googleEvent
	pageToken := ""
	for {
		u, err := url.Parse(eventsBaseURL + "/" + url.PathEscape(calendarID) + "/events")
		if err != nil {
			return nil, fmt.Errorf("build events url: %w", err)
		}
		q := u.Query()
		q.Set("timeMin", windowStart.Format(time.RFC3339))
		q.Set("timeMax", windowEnd.Format(time.RFC3339))
		q.Set("singleEvents", "true")
		q.Set("showDeleted", "false")
		q.Set("orderBy", "startTime")
		q.Set("maxResults", "250")
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("create events request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch google calendar events: %w", err)
		}
		var body eventsResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		closeErr := resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("fetch google calendar events: status %d", resp.StatusCode)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode google calendar events: %w", decodeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close google calendar events response: %w", closeErr)
		}
		events = append(events, body.Items...)
		if body.NextPageToken == "" {
			break
		}
		pageToken = body.NextPageToken
	}
	return events, nil
}

func convertEvent(event googleEvent, loc *time.Location) (model.SchoolClosure, bool) {
	if event.Status == "cancelled" || event.ID == "" || strings.TrimSpace(event.Summary) == "" {
		return model.SchoolClosure{}, false
	}
	if event.Start.Date != "" && event.End.Date != "" {
		start, err := time.Parse("2006-01-02", event.Start.Date)
		if err != nil {
			return model.SchoolClosure{}, false
		}
		end, err := time.Parse("2006-01-02", event.End.Date)
		if err != nil {
			return model.SchoolClosure{}, false
		}
		end = end.AddDate(0, 0, -1)
		if end.Before(start) {
			return model.SchoolClosure{}, false
		}
		return model.SchoolClosure{Title: event.Summary, StartDate: start.Format("2006-01-02"), EndDate: end.Format("2006-01-02"), GoogleEventID: event.ID}, true
	}
	if event.Start.DateTime == "" || event.End.DateTime == "" {
		return model.SchoolClosure{}, false
	}
	start, err := time.Parse(time.RFC3339, event.Start.DateTime)
	if err != nil {
		return model.SchoolClosure{}, false
	}
	end, err := time.Parse(time.RFC3339, event.End.DateTime)
	if err != nil {
		return model.SchoolClosure{}, false
	}
	startLocal := start.In(loc)
	endLocal := end.In(loc)
	if endLocal.Hour() == 0 && endLocal.Minute() == 0 && endLocal.Second() == 0 && endLocal.Nanosecond() == 0 {
		endLocal = endLocal.AddDate(0, 0, -1)
	}
	if endLocal.Before(startLocal) {
		return model.SchoolClosure{}, false
	}
	return model.SchoolClosure{Title: event.Summary, StartDate: startLocal.Format("2006-01-02"), EndDate: endLocal.Format("2006-01-02"), GoogleEventID: event.ID}, true
}
