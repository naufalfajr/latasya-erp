// Package school_calendar exposes school-calendar JSON endpoints.
package school_calendar

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/schoolcalendar"
)

type Handler struct {
	Calendar             *schoolcalendar.Module
	GoogleCalendarConfig googlecalendar.Config
}

type closureInput struct {
	Title     string `json:"title"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}
type effectiveDaysResponse struct {
	Month             string `json:"month"`
	EffectiveDays     int    `json:"effective_days"`
	MultiplierPercent int    `json:"multiplier_percent"`
}

func actor(r *http.Request) schoolcalendar.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return schoolcalendar.Actor{}
	}
	return schoolcalendar.Actor{UserID: u.ID, Username: u.Username, CanManage: v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage), IsAdmin: u.IsAdmin()}
}
func authorized(w http.ResponseWriter, r *http.Request) bool {
	if v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		return true
	}
	v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
	return false
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/school-calendar/closures", h.ListClosures)
	mux.HandleFunc("POST /api/v1/school-calendar/closures", h.CreateClosure)
	mux.HandleFunc("DELETE /api/v1/school-calendar/closures/{id}", h.DeleteClosure)
	mux.HandleFunc("GET /api/v1/school-calendar/effective-days", h.EffectiveDays)
	mux.HandleFunc("POST /api/v1/integrations/google-calendar/sync", h.SyncGoogleCalendar)
}

func (h *Handler) ListClosures(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month != "" {
		if _, err := time.Parse("2006-01", month); err != nil {
			v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid month", map[string]string{"month": "must be YYYY-MM"})
			return
		}
	}
	closures, err := h.Calendar.List(r.Context(), month)
	if err != nil {
		writeError(w, r, err)
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": closures})
}
func (h *Handler) CreateClosure(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	var input closureInput
	if err := v1.DecodeJSON(w, r, &input); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	closure, err := h.Calendar.CreateManual(r.Context(), actor(r), schoolcalendar.ClosureDraft{Title: input.Title, StartDate: input.StartDate, EndDate: input.EndDate})
	if err != nil {
		writeError(w, r, err)
		return
	}
	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": closure})
}
func (h *Handler) DeleteClosure(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "school closure not found", nil)
		return
	}
	if _, err := h.Calendar.Delete(r.Context(), actor(r), id); err != nil {
		writeError(w, r, err)
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"deleted": true}})
}
func (h *Handler) EffectiveDays(w http.ResponseWriter, r *http.Request) {
	if !authorized(w, r) {
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if _, err := time.Parse("2006-01", month); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid month", map[string]string{"month": "must be YYYY-MM"})
		return
	}
	days, err := h.Calendar.EffectiveDays(r.Context(), month)
	if err != nil {
		writeError(w, r, err)
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": effectiveDaysResponse{Month: month, EffectiveDays: days, MultiplierPercent: schoolcalendar.MultiplierPercent(days)}})
}
func (h *Handler) SyncGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	a := actor(r)
	if a.UserID == 0 {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}
	if !a.IsAdmin {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "admin user required", nil)
		return
	}
	if !a.CanManage {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	result, err := googlecalendar.Sync(r.Context(), h.Calendar, h.GoogleCalendarConfig, "")
	if err != nil {
		if errors.Is(err, googlecalendar.ErrNotConnected) {
			v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, googlecalendar.ErrNotConnected.Error(), nil)
		} else {
			slog.Error("google_calendar: api sync", "error", err)
			v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "google calendar sync failed", nil)
		}
		return
	}
	_ = h.Calendar.RecordSync(r.Context(), a, result.Fetched, result.Stored, result.WindowStart, result.WindowEnd)
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": result})
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *schoolcalendar.ValidationError
	switch {
	case errors.Is(err, schoolcalendar.ErrNotFound):
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "school closure not found", nil)
	case errors.Is(err, schoolcalendar.ErrForbidden):
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
	case errors.As(err, &validation):
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", validation.Fields)
	default:
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "school calendar operation failed", nil)
	}
}
