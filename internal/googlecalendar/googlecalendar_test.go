package googlecalendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

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
