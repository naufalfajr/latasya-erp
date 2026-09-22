package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type journalViewData struct {
	Entry     *model.JournalEntry
	BackURL   string
	EditURL   string
	DeleteURL string
	CanMutate bool
	ReturnTo  string
}

func journalSourceList(source string) string {
	switch source {
	case model.SourceIncome:
		return "/income"
	case model.SourceExpense:
		return "/expenses"
	default:
		return "/journals"
	}
}

func validJournalReturnPath(path string) bool {
	switch path {
	case "/journals", "/income", "/expenses":
		return true
	default:
		return false
	}
}

// safeJournalReturnTo accepts only same-app list URLs. Restricting the path to
// the three accounting lists prevents open redirects while preserving each
// list's query string and pagination context.
func safeJournalReturnTo(value, source, basePath string) string {
	fallback := journalSourceList(source)
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\\\r\n") {
		return fallback
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Opaque != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.Fragment != "" {
		return fallback
	}
	path := parsed.Path
	basePath = strings.TrimSuffix(basePath, "/")
	if basePath != "" {
		for _, route := range []string{"/journals", "/income", "/expenses"} {
			if path == basePath+route {
				path = route
				break
			}
		}
	}
	if !validJournalReturnPath(path) {
		return fallback
	}
	if parsed.RawQuery != "" {
		return path + "?" + parsed.RawQuery
	}
	return path
}

func journalReturnToFromRequest(r *http.Request, source, basePath string) string {
	values := r.URL.Query()["return_to"]
	if len(values) != 1 {
		return journalSourceList(source)
	}
	return safeJournalReturnTo(values[0], source, basePath)
}

func journalEditReturnToFromRequest(r *http.Request, source, basePath string) string {
	values := r.URL.Query()["return_to"]
	if len(values) != 1 || values[0] == "" {
		return ""
	}
	return safeJournalReturnTo(values[0], source, basePath)
}

func journalEditReturnToFromForm(r *http.Request, source, basePath string) string {
	if err := r.ParseForm(); err != nil {
		return ""
	}
	values := r.PostForm["return_to"]
	if len(values) != 1 || values[0] == "" {
		return ""
	}
	return safeJournalReturnTo(values[0], source, basePath)
}

func (h *Handler) journalUpdatedDetailURL(id int, r *http.Request, source string) string {
	returnTo := journalEditReturnToFromForm(r, source, h.BasePath)
	if returnTo == "" {
		return h.BasePath + fmt.Sprintf("/journals/%d", id)
	}
	return h.journalDetailURL(id, returnTo)
}

func withJournalReturnTo(path, returnTo string) string {
	query := url.Values{}
	query.Set("return_to", returnTo)
	return path + "?" + query.Encode()
}

func (h *Handler) journalDetailURL(id int, returnTo string) string {
	return withJournalReturnTo(h.BasePath+fmt.Sprintf("/journals/%d", id), returnTo)
}

func (h *Handler) journalViewData(r *http.Request, entry *model.JournalEntry) journalViewData {
	returnTo := journalReturnToFromRequest(r, entry.SourceType, h.BasePath)
	data := journalViewData{
		Entry:    entry,
		BackURL:  h.BasePath + returnTo,
		ReturnTo: returnTo,
	}
	user := userFromRequest(r)
	switch entry.SourceType {
	case "", model.SourceManual:
		data.CanMutate = user != nil && user.HasCapability(model.CapJournalsManage)
		data.EditURL = withJournalReturnTo(h.BasePath+fmt.Sprintf("/journals/%d/edit", entry.ID), returnTo)
		data.DeleteURL = withJournalReturnTo(h.BasePath+fmt.Sprintf("/journals/%d", entry.ID), returnTo)
	case model.SourceIncome:
		data.CanMutate = user != nil && user.HasCapability(model.CapIncomeManage)
		data.EditURL = withJournalReturnTo(h.BasePath+fmt.Sprintf("/income/%d/edit", entry.ID), returnTo)
		data.DeleteURL = withJournalReturnTo(h.BasePath+fmt.Sprintf("/income/%d", entry.ID), returnTo)
	case model.SourceExpense:
		data.CanMutate = user != nil && user.HasCapability(model.CapExpensesManage)
		data.EditURL = withJournalReturnTo(h.BasePath+fmt.Sprintf("/expenses/%d/edit", entry.ID), returnTo)
		data.DeleteURL = withJournalReturnTo(h.BasePath+fmt.Sprintf("/expenses/%d", entry.ID), returnTo)
	}
	return data
}

func userFromRequest(r *http.Request) *model.User {
	return auth.UserFromContext(r.Context())
}

func (h *Handler) finishJournalDelete(w http.ResponseWriter, r *http.Request, destination string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", destination)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}
