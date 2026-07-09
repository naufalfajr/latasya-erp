// Package school_calendar implements the /api/v1/school-calendar endpoints.
package school_calendar

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct {
	DB                   *sql.DB
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

func (h *Handler) ListClosures(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}

	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month != "" {
		if _, err := time.Parse("2006-01", month); err != nil {
			v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid month", map[string]string{"month": "must be YYYY-MM"})
			return
		}
	}

	closures, err := model.ListSchoolClosures(h.DB, month)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list school closures", nil)
		return
	}
	if closures == nil {
		closures = []model.SchoolClosure{}
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": closures})
}

func (h *Handler) CreateClosure(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}

	var inp closureInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	closure := model.SchoolClosure{
		Source:    model.SchoolClosureSourceManual,
		Title:     strings.TrimSpace(inp.Title),
		StartDate: strings.TrimSpace(inp.StartDate),
		EndDate:   strings.TrimSpace(inp.EndDate),
	}
	if fields := validateClosure(closure); len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	id, err := model.CreateSchoolClosure(h.DB, &closure)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create school closure", nil)
		return
	}
	created, err := model.GetSchoolClosure(h.DB, id)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load created school closure", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "school_closure.create",
		TargetType:  "school_closure",
		TargetID:    int64(id),
		TargetLabel: created.Title,
		Metadata: map[string]any{"after": map[string]any{
			"source":     created.Source,
			"title":      created.Title,
			"start_date": created.StartDate,
			"end_date":   created.EndDate,
		}},
	})

	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": created})
}

func (h *Handler) DeleteClosure(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "school closure not found", nil)
		return
	}
	existing, err := model.GetSchoolClosure(h.DB, id)
	if errors.Is(err, sql.ErrNoRows) {
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "school closure not found", nil)
		return
	}
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to load school closure", nil)
		return
	}
	if err := model.DeleteSchoolClosure(h.DB, id); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to delete school closure", nil)
		return
	}

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

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"deleted": true}})
}

func (h *Handler) EffectiveDays(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if _, err := time.Parse("2006-01", month); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid month", map[string]string{"month": "must be YYYY-MM"})
		return
	}

	days, err := model.EffectiveSchoolDays(h.DB, month)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to calculate effective school days", nil)
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": effectiveDaysResponse{
		Month:             month,
		EffectiveDays:     days,
		MultiplierPercent: model.MonthlyPriceMultiplierPercent(days),
	}})
}

func (h *Handler) SyncGoogleCalendar(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}
	if user.Role != model.RoleAdmin {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "admin user required", nil)
		return
	}
	if !v1.HasEffectiveCapability(r.Context(), model.CapInvoicesManage) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "invoices.manage capability required", nil)
		return
	}

	result, err := googlecalendar.Sync(r.Context(), h.DB, h.GoogleCalendarConfig, "")
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, err.Error(), nil)
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
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": result})
}

func validateClosure(closure model.SchoolClosure) map[string]string {
	fields := map[string]string{}
	if closure.Title == "" {
		fields["title"] = "required"
	}
	start, startErr := time.Parse("2006-01-02", closure.StartDate)
	if closure.StartDate == "" || startErr != nil {
		fields["start_date"] = "must be YYYY-MM-DD"
	}
	end, endErr := time.Parse("2006-01-02", closure.EndDate)
	if closure.EndDate == "" || endErr != nil {
		fields["end_date"] = "must be YYYY-MM-DD"
	}
	if startErr == nil && endErr == nil && start.After(end) {
		fields["end_date"] = "must be on or after start_date"
	}
	return fields
}
