package handler

import (
	"database/sql"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"sync"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/apitoken"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/bill"
	"github.com/naufal/latasya-erp/internal/company"
	"github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/creditnote"
	"github.com/naufal/latasya-erp/internal/googlecalendar"
	"github.com/naufal/latasya-erp/internal/invoice"
	"github.com/naufal/latasya-erp/internal/journal"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/reporting"
	"github.com/naufal/latasya-erp/internal/schoolcalendar"
)

type Handler struct {
	DB         *sql.DB
	TemplateFS embed.FS
	FuncMap    template.FuncMap
	DevMode    bool

	// BasePath prefixes every internal redirect and template link, so the
	// admin app can be mounted under a path other than "/" (e.g.
	// "/dashboard" in production) without hardcoding it into every handler.
	// Zero value "" keeps routes at the root, which is what tests use.
	BasePath string

	GoogleCalendarConfig googlecalendar.Config
	Invoices             *invoice.Module
	Journals             *journal.Module
	Bills                *bill.Module
	CreditNotes          *creditnote.Module
	Accounts             *account.Module
	Access               *access.Module
	APITokens            *apitoken.Module
	Audit                *audit.Module
	SchoolCalendar       *schoolcalendar.Module
	Contacts             *contact.Module
	Company              *company.Module
	Reporting            *reporting.Module

	mu    sync.RWMutex
	cache map[string]*template.Template
}

type PageData struct {
	User      *model.User
	Title     string
	Flash     string
	Path      string
	CSRFToken string
	BasePath  string
	Data      any
}

// shared templates that every page includes
var sharedTemplates = []string{
	"templates/base.html",
	"templates/partials/nav.html",
	"templates/partials/sidebar.html",
	"templates/partials/flash.html",
	"templates/partials/csrf.html",
	"templates/partials/pagination.html",
}

func (h *Handler) getTemplate(pages ...string) (*template.Template, error) {
	cacheKey := pages[0]
	if !h.DevMode {
		h.mu.RLock()
		if t, ok := h.cache[cacheKey]; ok {
			h.mu.RUnlock()
			return t, nil
		}
		h.mu.RUnlock()
	}

	files := make([]string, len(sharedTemplates)+len(pages))
	copy(files, sharedTemplates)
	copy(files[len(sharedTemplates):], pages)

	t, err := template.New("").Funcs(h.FuncMap).ParseFS(h.TemplateFS, files...)
	if err != nil {
		return nil, err
	}

	if !h.DevMode {
		h.mu.Lock()
		if h.cache == nil {
			h.cache = make(map[string]*template.Template)
		}
		h.cache[cacheKey] = t
		h.mu.Unlock()
	}

	return t, nil
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, page string, title string, data any, extraTemplates ...string) {
	pd := PageData{
		User:      auth.UserFromContext(r.Context()),
		Title:     title,
		Path:      r.URL.Path,
		CSRFToken: auth.CSRFFromContext(r.Context()),
		BasePath:  h.BasePath,
		Data:      data,
	}

	if cookie, err := r.Cookie("flash"); err == nil {
		pd.Flash = cookie.Value
		http.SetCookie(w, &http.Cookie{
			Name:   "flash",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
	}

	pages := append([]string{page}, extraTemplates...)
	t, err := h.getTemplate(pages...)
	if err != nil {
		slog.Error("parse template", "page", page, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := t.ExecuteTemplate(w, "base", pd); err != nil {
		slog.Error("render template", "page", page, "error", err)
	}
}

func (h *Handler) renderFragment(w http.ResponseWriter, r *http.Request, page, name string, data any) {
	t, err := h.getTemplate(page)
	if err != nil {
		slog.Error("parse fragment template", "page", page, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	pd := PageData{User: auth.UserFromContext(r.Context()), Path: r.URL.Path, CSRFToken: auth.CSRFFromContext(r.Context()), BasePath: h.BasePath, Data: data}
	if err := t.ExecuteTemplate(w, name, pd); err != nil {
		slog.Error("render fragment", "page", page, "fragment", name, "error", err)
	}
}

func isHTMXTarget(r *http.Request, id string) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == id
}

func (h *Handler) setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "flash",
		Value:    msg,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
