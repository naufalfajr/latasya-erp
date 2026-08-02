package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/schoolcalendar"
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
	data, err := h.schoolCalendarPageData(r.Context(), month, model.SchoolClosure{}, map[string]string{})
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
		data, err := h.schoolCalendarPageData(r.Context(), month, closure, errs)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		h.render(w, r, "templates/settings/school_calendar.html", "School Calendar", data)
		return
	}

	created, err := h.SchoolCalendar.CreateManual(r.Context(), schoolActor(r), schoolcalendar.ClosureDraft{Title: closure.Title, StartDate: closure.StartDate, EndDate: closure.EndDate})
	if err != nil {
		data, loadErr := h.schoolCalendarPageData(r.Context(), month, closure, map[string]string{"general": "Failed to add closure"})
		if loadErr != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		h.render(w, r, "templates/settings/school_calendar.html", "School Calendar", data)
		return
	}

	_ = created

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
	if _, err := h.SchoolCalendar.Delete(r.Context(), schoolActor(r), id); err != nil && !errors.Is(err, schoolcalendar.ErrNotFound) {
		h.setFlash(w, "Error deleting closure: "+err.Error())
		h.redirectSchoolCalendar(w, r, month)
		return
	}
	h.setFlash(w, "School closure deleted")
	h.redirectSchoolCalendar(w, r, month)
}

func (h *Handler) SaveGoogleCalendarID(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	if _, err := h.SchoolCalendar.Connection(r.Context()); err != nil {
		h.setFlash(w, "Error loading Google Calendar settings: "+err.Error())
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	if _, err := h.SchoolCalendar.SaveCalendarID(r.Context(), schoolActor(r), r.FormValue("calendar_id")); err != nil {
		h.setFlash(w, "Error saving Google Calendar ID: "+err.Error())
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	h.setFlash(w, "Google Calendar ID saved")
	h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
}

func (h *Handler) ConnectGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	if !h.GoogleCalendarConfig.Enabled() {
		h.setFlash(w, "Google Calendar OAuth is not configured")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	state, err := randomOAuthState()
	if err != nil {
		h.setFlash(w, "Error starting Google connection")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	verifier := googlecalendar.GeneratePKCEVerifier()
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	if err := h.SchoolCalendar.CreateOAuthState(r.Context(), schoolActor(r), state, verifier, expiresAt); err != nil {
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

	state, err := h.SchoolCalendar.ConsumeOAuthState(r.Context(), schoolActor(r), stateValue)
	if errors.Is(err, schoolcalendar.ErrNotFound) {
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

	conn, err := h.SchoolCalendar.Connection(r.Context())
	if err != nil {
		h.setFlash(w, "Error loading Google Calendar settings")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	if token.RefreshToken == "" {
		token.RefreshToken = conn.RefreshToken
	}
	if token.RefreshToken == "" {
		h.setFlash(w, "Google did not return a refresh token. Please reconnect and approve offline access.")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	if _, err := h.SchoolCalendar.Connect(r.Context(), schoolActor(r), token.RefreshToken); err != nil {
		h.setFlash(w, "Error saving Google Calendar connection")
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	h.setFlash(w, "Google Calendar connected")
	h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
}

func (h *Handler) SyncGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	result, err := googlecalendar.Sync(r.Context(), h.SchoolCalendar, h.GoogleCalendarConfig, "")
	if err != nil {
		if errors.Is(err, googlecalendar.ErrNotConnected) {
			h.setFlash(w, "Google Calendar sync failed: "+googlecalendar.ErrNotConnected.Error())
		} else {
			slog.Error("google_calendar: sync", "error", err)
			h.setFlash(w, "Google Calendar sync failed")
		}
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}

	_ = h.SchoolCalendar.RecordSync(r.Context(), schoolActor(r), result.Fetched, result.Stored, result.WindowStart, result.WindowEnd)
	h.setFlash(w, fmt.Sprintf("Google Calendar synced: fetched %d event(s), stored %d closure(s).", result.Fetched, result.Stored))
	h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
}

func (h *Handler) DisconnectGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	if err := h.SchoolCalendar.Disconnect(r.Context(), schoolActor(r)); err != nil {
		h.setFlash(w, "Error disconnecting Google Calendar: "+err.Error())
		h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
		return
	}
	h.setFlash(w, "Google Calendar disconnected")
	h.redirectSchoolCalendar(w, r, schoolCalendarMonth(r))
}

func (h *Handler) schoolCalendarPageData(ctx context.Context, month string, manual model.SchoolClosure, errs map[string]string) (schoolCalendarPageData, error) {
	closures, err := h.SchoolCalendar.List(ctx, month)
	if err != nil {
		return schoolCalendarPageData{}, err
	}
	days, err := h.SchoolCalendar.EffectiveDays(ctx, month)
	if err != nil {
		return schoolCalendarPageData{}, err
	}
	conn, err := h.SchoolCalendar.Connection(ctx)
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
		MultiplierPercent:   schoolcalendar.MultiplierPercent(days),
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
	http.Redirect(w, r, h.BasePath+"/settings/school-calendar?month="+month, http.StatusSeeOther)
}

func schoolActor(r *http.Request) schoolcalendar.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return schoolcalendar.Actor{}
	}
	return schoolcalendar.Actor{UserID: u.ID, Username: u.Username, CanManage: u.HasCapability(model.CapInvoicesManage), IsAdmin: u.IsAdmin()}
}

func randomOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
