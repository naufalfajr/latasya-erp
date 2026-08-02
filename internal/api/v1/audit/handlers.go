package auditapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Audit *audit.Module }

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/audit", h.List)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	canView := v1.HasEffectiveCapability(r.Context(), model.CapAuditView)
	actor := audit.Actor{CanView: canView}
	if user != nil {
		actor.UserID = user.ID
	}
	q := r.URL.Query()
	page := v1.ParsePage(r)
	filter := audit.ListFilter{ActorUsername: strings.TrimSpace(q.Get("actor")), ActionPrefix: strings.TrimSpace(q.Get("action")), Limit: page.PerPage, Offset: page.Offset()}
	if value := strings.TrimSpace(q.Get("from")); value != "" {
		if parsed, err := time.Parse("2006-01-02", value); err == nil {
			filter.From = parsed
		}
	}
	if value := strings.TrimSpace(q.Get("to")); value != "" {
		if parsed, err := time.Parse("2006-01-02", value); err == nil {
			filter.To = parsed.Add(24*time.Hour - time.Millisecond)
		}
	}
	result, err := h.Audit.List(r.Context(), actor, filter)
	if errors.Is(err, audit.ErrForbidden) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list audit log", nil)
		return
	}
	v1.WriteList(w, http.StatusOK, result.Entries, page, result.Total)
}
