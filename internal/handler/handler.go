package handler

import (
	"database/sql"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"sync"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct {
	DB         *sql.DB
	TemplateFS embed.FS
	FuncMap    template.FuncMap
	DevMode    bool

	mu    sync.RWMutex
	cache map[string]*template.Template
}

type PageData struct {
	User  *model.User
	Title string
	Flash string
	Path  string
	Data  any
}

// shared templates that every page includes
var sharedTemplates = []string{
	"templates/base.html",
	"templates/partials/nav.html",
	"templates/partials/sidebar.html",
	"templates/partials/flash.html",
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
		User:  auth.UserFromContext(r.Context()),
		Title: title,
		Path:  r.URL.Path,
		Data:  data,
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

func (h *Handler) setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "flash",
		Value:    msg,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
