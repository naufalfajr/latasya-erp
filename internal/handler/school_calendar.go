package handler

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/model"
)

type schoolCalendarPageData struct {
	Month                  string
	Closures               []model.SchoolClosure
	EffectiveSchoolDays    int
	MultiplierPercent      int
	GoogleConnection       *googleCalendarConnectionView
	GoogleConfigEnabled    bool
	GoogleConnectionActive bool
	GoogleCalendarID       string
	GoogleCalendarIDSaved  bool
	ManualClosure          model.SchoolClosure
	Errors                 map[string]string
}

type googleCalendarConnectionView struct {
	IsActive       bool
	LastSyncAt     string
	LastSyncStatus string
	LastSyncError  string
}

func (h *Handler) SchoolCalendarPage(w http.ResponseWriter, r *http.Request) {
	month := schoolCalendarMonth(r)
	data, err := h.schoolCalendarPageData(month, model.SchoolClosure{}, map[string]string{})
	if err != nil {
		slog.Error("school_calendar: load", "error", err)
		h.render(w, r, "templates/settings/school_calendar.html", "School Calendar", schoolCalendarPageData{
			Month:               month,
			GoogleConfigEnabled: h.GoogleCalendarConfig.Enabled(),
			Errors:              map[string]string{"general": "Failed to load school calendar settings"},
		})
		return
	}

	h.render(w, r, "templates/settings/school_calendar.html", "School Calendar", data)
}

func (h *Handler) CreateSchoolClosure(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	closure := model.SchoolClosure{
		Source:    model.SchoolClosureSourceManual,
		Title:     strings.TrimSpace(r.FormValue("title")),
		StartDate: strings.TrimSpace(r.FormValue("start_date")),
		EndDate:   strings.TrimSpace(r.FormValue("end_date")),
	}
	month := monthFromDateOrRequest(closure.StartDate, r)
	errs := validateManualSchoolClosure(closure)
	if len(errs) > 0 {
		data, err := h.schoolCalendarPageData(month, closure, errs)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		h.render(w, r, "templates/settings/school_calendar.html", "School Calendar", data)
		return
	}

	id, err := model.CreateSchoolClosure(h.DB, &closure)
	if err != nil {
		data, loadErr := h.schoolCalendarPageData(month, closure, map[string]string{"general": "Failed to add closure"})
		if loadErr != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		h.render(w, r, "templates/settings/school_calendar.html", "School Calendar", data)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "school_closure.create",
		TargetType:  "school_closure",
		TargetID:    int64(id),
		TargetLabel: closure.Title,
		Metadata: map[string]any{"after": map[string]any{
			"source":     closure.Source,
			"title":      closure.Title,
			"start_date": closure.StartDate,
			"end_date":   closure.EndDate,
		}},
	})

	h.setFlash(w, "School closure added")
	h.redirectSchoolCalendar(w, r, month)
}

func (h *Handler) DeleteSchoolClosure(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	month := schoolCalendarMonth(r)
	existing, _ := model.GetSchoolClosure(h.DB, id)
	if err := model.DeleteSchoolClosure(h.DB, id); err != nil {
		h.setFlash(w, "Error deleting closure: "+err.Error())
		h.redirectSchoolCalendar(w, r, month)
		return
	}
	if existing != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:      "school_closure.delete",
			TargetType:  "school_closure",
			TargetID:    int64(id),
			TargetLabel: existing.Title,
			Metadata: map[string]any{"before": map[string]any{
				"source":     existing.Source,
				"title":      existing.Title,
				"start_date": existing.StartDate,
				"end_date":   existing.EndDate,
			}},
		})
	}

	h.setFlash(w, "School closure deleted")
	h.redirectSchoolCalendar(w, r, month)
}

func (h *Handler) SaveGoogleCalendarID(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	conn, err := model.GetGoogleCalendarConnection(h.DB)
	if err != nil {
		h.setFlash(w, "Error loading Google Calendar settings: "+err.Error())
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	conn.CalendarID = strings.TrimSpace(r.FormValue("calendar_id"))
	if err := model.SaveGoogleCalendarConnection(h.DB, conn); err != nil {
		h.setFlash(w, "Error saving Google Calendar ID: "+err.Error())
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:     "google_calendar.calendar_id.update",
		TargetType: "google_calendar_connection",
		TargetID:   1,
		Metadata:   map[string]any{"calendar_id_set": conn.CalendarID != ""},
	})
	h.setFlash(w, "Google Calendar ID saved")
	h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
}

func (h *Handler) ConnectGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	if !h.GoogleCalendarConfig.Enabled() {
		h.setFlash(w, "Google Calendar OAuth is not configured")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	user := auth.UserFromContext(r.Context())
	state, err := randomOAuthState()
	if err != nil {
		h.setFlash(w, "Error starting Google connection")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	verifier := googlecalendar.GeneratePKCEVerifier()
	expiresAt := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	if err := model.CreateGoogleOAuthState(h.DB, state, user.ID, verifier, expiresAt); err != nil {
		h.setFlash(w, "Error starting Google connection")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	http.Redirect(w, r, h.GoogleCalendarConfig.OAuthURL(state, verifier), http.StatusSeeOther)
}

func (h *Handler) GoogleCalendarCallback(w http.ResponseWriter, r *http.Request) {
	if !h.GoogleCalendarConfig.Enabled() {
		h.setFlash(w, "Google Calendar OAuth is not configured")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	if r.URL.Query().Get("error") != "" {
		h.setFlash(w, "Google Calendar connection cancelled")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	stateValue := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || stateValue == "" {
		h.setFlash(w, "Google Calendar callback was missing required values")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	user := auth.UserFromContext(r.Context())
	state, err := model.ConsumeGoogleOAuthState(h.DB, stateValue, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		h.setFlash(w, "Google Calendar connection expired. Please try again.")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	if err != nil {
		h.setFlash(w, "Error validating Google connection")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	token, err := h.GoogleCalendarConfig.Exchange(r.Context(), code, state.PKCEVerifier)
	if err != nil {
		slog.Error("google_calendar: exchange", "error", err)
		h.setFlash(w, "Google Calendar authorization failed")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	conn, err := model.GetGoogleCalendarConnection(h.DB)
	if err != nil {
		h.setFlash(w, "Error loading Google Calendar settings")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	refreshToken := token.RefreshToken
	if refreshToken == "" {
		refreshToken = conn.RefreshToken
	}
	if refreshToken == "" {
		h.setFlash(w, "Google did not return a refresh token. Please reconnect and approve offline access.")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	conn.RefreshToken = refreshToken
	conn.IsActive = true
	conn.LastSyncStatus = ""
	conn.LastSyncError = ""
	if err := model.SaveGoogleCalendarConnection(h.DB, conn); err != nil {
		h.setFlash(w, "Error saving Google Calendar connection")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:     "google_calendar.connect",
		TargetType: "google_calendar_connection",
		TargetID:   1,
		Metadata:   map[string]any{"calendar_id_set": conn.CalendarID != ""},
	})
	h.setFlash(w, "Google Calendar connected")
	h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
}

func (h *Handler) SyncGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	result, err := googlecalendar.Sync(r.Context(), h.DB, h.GoogleCalendarConfig, "")
	if err != nil {
		h.setFlash(w, "Google Calendar sync failed: "+err.Error())
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:     "google_calendar.sync",
		TargetType: "google_calendar_connection",
		TargetID:   1,
		Metadata: map[string]any{
			"fetched":      result.Fetched,
			"stored":       result.Stored,
			"window_start": result.WindowStart,
			"window_end":   result.WindowEnd,
		},
	})
	h.setFlash(w, fmt.Sprintf("Google Calendar synced: fetched %d event(s), stored %d closure(s).", result.Fetched, result.Stored))
	h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
}

func (h *Handler) DisconnectGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	if err := model.DeleteGoogleCalendarConnection(h.DB); err != nil {
		h.setFlash(w, "Error disconnecting Google Calendar: "+err.Error())
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	audit.Log(r.Context(), h.DB, audit.Event{
		Action:     "google_calendar.disconnect",
		TargetType: "google_calendar_connection",
		TargetID:   1,
	})
	h.setFlash(w, "Google Calendar disconnected")
	h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
}

func (h *Handler) schoolCalendarPageData(month string, manual model.SchoolClosure, errs map[string]string) (schoolCalendarPageData, error) {
	closures, err := model.ListSchoolClosures(h.DB, month)
	if err != nil {
		return schoolCalendarPageData{}, err
	}
	days, err := model.EffectiveSchoolDays(h.DB, month)
	if err != nil {
		return schoolCalendarPageData{}, err
	}
	conn, err := model.GetGoogleCalendarConnection(h.DB)
	if err != nil {
		return schoolCalendarPageData{}, err
	}
	if errs == nil {
		errs = map[string]string{}
	}
	return schoolCalendarPageData{
		Month:               month,
		Closures:            closures,
		EffectiveSchoolDays: days,
		MultiplierPercent:   model.MonthlyPriceMultiplierPercent(days),
		GoogleConnection: &googleCalendarConnectionView{
			IsActive:       conn.IsActive,
			LastSyncAt:     conn.LastSyncAt,
			LastSyncStatus: conn.LastSyncStatus,
			LastSyncError:  conn.LastSyncError,
		},
		GoogleConfigEnabled:    h.GoogleCalendarConfig.Enabled(),
		GoogleConnectionActive: conn.IsActive && conn.RefreshToken != "",
		GoogleCalendarID:       conn.CalendarID,
		GoogleCalendarIDSaved:  conn.CalendarID != "",
		ManualClosure:          manual,
		Errors:                 errs,
	}, nil
}

func validateManualSchoolClosure(closure model.SchoolClosure) map[string]string {
	errs := map[string]string{}
	if closure.Title == "" {
		errs["title"] = "Title is required"
	}
	start, startErr := time.Parse("2006-01-02", closure.StartDate)
	if closure.StartDate == "" || startErr != nil {
		errs["start_date"] = "Valid start date is required"
	}
	end, endErr := time.Parse("2006-01-02", closure.EndDate)
	if closure.EndDate == "" || endErr != nil {
		errs["end_date"] = "Valid end date is required"
	}
	if startErr == nil && endErr == nil && start.After(end) {
		errs["end_date"] = "End date must be on or after start date"
	}
	return errs
}

func schoolCalendarMonth(r *http.Request) string {
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if _, err := time.Parse("2006-01", month); err == nil {
		return month
	}
	return time.Now().Format("2006-01")
}

func monthFromDateOrRequest(date string, r *http.Request) string {
	if parsed, err := time.Parse("2006-01-02", date); err == nil {
		return parsed.Format("2006-01")
	}
	return schoolCalendarMonth(r)
}

func (h *Handler) redirectSchoolCalendar(w http.ResponseWriter, r *http.Request, month string) {
	if _, err := time.Parse("2006-01", month); err != nil {
		month = time.Now().Format("2006-01")
	}
	http.Redirect(w, r, "/settings/school-calendar?month="+month, http.StatusSeeOther)
}

func randomOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
