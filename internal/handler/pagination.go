package handler

import (
	"net/http"
	"net/url"
	"strconv"
)

// listPageSize is the fixed page size for the paginated list pages, matching
// the audit log's pageSize.
const listPageSize = 50

// Pagination carries page metadata to the list templates. Its methods are used
// directly in templates to render the "Page X of Y" + Prev/Next controls.
type Pagination struct {
	Page       int
	PageSize   int
	Total      int
	TotalPages int
}

// parsePage reads the ?page= query param, defaulting to 1.
func parsePage(r *http.Request) int {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	return page
}

// newPagination builds the page metadata for a given page and total row count.
func newPagination(page, total int) Pagination {
	totalPages := (total + listPageSize - 1) / listPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	return Pagination{Page: page, PageSize: listPageSize, Total: total, TotalPages: totalPages}
}

// Offset is the SQL OFFSET for the current page.
func (p Pagination) Offset() int { return (p.Page - 1) * p.PageSize }

func (p Pagination) HasPrev() bool { return p.Page > 1 }
func (p Pagination) HasNext() bool { return p.Page < p.TotalPages }
func (p Pagination) PrevPage() int { return p.Page - 1 }
func (p Pagination) NextPage() int { return p.Page + 1 }

// PageNav couples pagination metadata with the active filter params so the
// shared "pagination" template partial can render filter-preserving page links.
// Used by the full-page (non-htmx) list pages.
type PageNav struct {
	Pagination
	params url.Values
}

// newPageNav pairs page metadata with the current filters. Empty filter values
// are dropped from the generated links.
func newPageNav(pg Pagination, params map[string]string) PageNav {
	q := url.Values{}
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	return PageNav{Pagination: pg, params: q}
}

func (n PageNav) pageURL(page int) string {
	q := url.Values{}
	for k, v := range n.params {
		q[k] = v
	}
	q.Set("page", strconv.Itoa(page))
	return "?" + q.Encode()
}

// PrevURL / NextURL are the filter-preserving links used by the partial.
func (n PageNav) PrevURL() string { return n.pageURL(n.PrevPage()) }
func (n PageNav) NextURL() string { return n.pageURL(n.NextPage()) }
