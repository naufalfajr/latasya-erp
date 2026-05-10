package auditapi

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	auditstore "github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct {
	DB *sql.DB
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if !v1.HasEffectiveCapability(r.Context(), model.CapAuditView) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
		return
	}

	q := r.URL.Query()
	actor := strings.TrimSpace(q.Get("actor"))
	action := strings.TrimSpace(q.Get("action"))
	fromStr := strings.TrimSpace(q.Get("from"))
	toStr := strings.TrimSpace(q.Get("to"))

	page := v1.ParsePage(r)

	filter := auditstore.ListFilter{
		ActorUsername: actor,
		ActionPrefix:  action,
		Limit:         page.PerPage,
		Offset:        page.Offset(),
	}
	if fromStr != "" {
		if t, err := time.Parse("2006-01-02", fromStr); err == nil {
			filter.From = t
		}
	}
	if toStr != "" {
		if t, err := time.Parse("2006-01-02", toStr); err == nil {
			filter.To = t.Add(24*time.Hour - time.Millisecond)
		}
	}

	entries, total, err := auditstore.List(h.DB, filter)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list audit log", nil)
		return
	}
	if entries == nil {
		entries = []auditstore.Entry{}
	}

	v1.WriteList(w, http.StatusOK, entries, page, total)
}
