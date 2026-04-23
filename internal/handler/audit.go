package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
)

type auditListData struct {
	Entries    []audit.Entry
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	Filter     auditFilterForm
}

type auditFilterForm struct {
	Actor  string
	Action string
	From   string // YYYY-MM-DD
	To     string // YYYY-MM-DD
}

func (h *Handler) AuditList(w http.ResponseWriter, r *http.Request) {
	const pageSize = 50

	q := r.URL.Query()
	form := auditFilterForm{
		Actor:  strings.TrimSpace(q.Get("actor")),
		Action: strings.TrimSpace(q.Get("action")),
		From:   strings.TrimSpace(q.Get("from")),
		To:     strings.TrimSpace(q.Get("to")),
	}

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	filter := audit.ListFilter{
		ActorUsername: form.Actor,
		ActionPrefix:  form.Action,
		Limit:         pageSize,
		Offset:        (page - 1) * pageSize,
	}
	if form.From != "" {
		if t, err := time.Parse("2006-01-02", form.From); err == nil {
			filter.From = t
		}
	}
	if form.To != "" {
		if t, err := time.Parse("2006-01-02", form.To); err == nil {
			// Interpret "to" as end-of-day (inclusive).
			filter.To = t.Add(24*time.Hour - time.Millisecond)
		}
	}

	entries, total, err := audit.List(h.DB, filter)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	h.render(w, r, "templates/audit/list.html", "Audit Log", auditListData{
		Entries:    entries,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
		Filter:     form,
	})
}
