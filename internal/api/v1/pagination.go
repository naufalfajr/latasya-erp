package v1

import (
	"math"
	"net/http"
	"strconv"
)

const (
	defaultPerPage = 50
	maxPerPage     = 200
)

// Page holds parsed pagination parameters.
type Page struct {
	PageNum int
	PerPage int
}

// Meta is the pagination metadata returned in list responses.
type Meta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// ListEnvelope is the standard JSON list response shape.
type ListEnvelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// ParsePage reads ?page= and ?per_page= from the request.
// Invalid or out-of-range values are silently clamped to defaults.
// page: min 1, default 1
// per_page: min 1, max 200, default 50
func ParsePage(r *http.Request) Page {
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p >= 1 {
		page = p
	}
	perPage := defaultPerPage
	if pp, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil {
		if pp < 1 {
			perPage = defaultPerPage
		} else if pp > maxPerPage {
			perPage = maxPerPage
		} else {
			perPage = pp
		}
	}
	return Page{PageNum: page, PerPage: perPage}
}

// Offset returns the SQL OFFSET for this page.
func (p Page) Offset() int {
	return (p.PageNum - 1) * p.PerPage
}

// BuildMeta computes pagination metadata given total item count.
func BuildMeta(page Page, total int) Meta {
	totalPages := 0
	if total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(page.PerPage)))
	}
	return Meta{
		Page:       page.PageNum,
		PerPage:    page.PerPage,
		Total:      total,
		TotalPages: totalPages,
	}
}

// WriteList writes a paginated JSON list response.
func WriteList(w http.ResponseWriter, status int, data any, page Page, total int) {
	WriteJSON(w, status, ListEnvelope{
		Data: data,
		Meta: BuildMeta(page, total),
	})
}
