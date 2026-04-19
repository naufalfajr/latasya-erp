package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

const minPasswordLength = 8

// EnforcePasswordChange redirects any authenticated user whose account has
// must_change_password=1 to the forced-change page, except when they are
// already on it.
func EnforcePasswordChange(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := auth.UserFromContext(r.Context())
		if user != nil && user.MustChangePassword && r.URL.Path != "/password/change" {
			http.Redirect(w, r, "/password/change", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type passwordChangeData struct {
	Forced bool
	Error  string
}

func (h *Handler) PasswordChangePage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	h.render(w, r, "templates/auth/password_change.html", "Change Password", passwordChangeData{
		Forced: user != nil && user.MustChangePassword,
	})
}

func (h *Handler) PasswordChange(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	current := r.FormValue("current_password")
	next := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	render := func(msg string) {
		h.render(w, r, "templates/auth/password_change.html", "Change Password", passwordChangeData{
			Forced: user.MustChangePassword,
			Error:  msg,
		})
	}

	if !auth.CheckPassword(user.Password, current) {
		render("Current password is incorrect")
		return
	}
	if len(next) < minPasswordLength {
		render("New password must be at least 8 characters")
		return
	}
	if next != confirm {
		render("New password and confirmation do not match")
		return
	}
	if next == current {
		render("New password must be different from current password")
		return
	}

	hash, err := auth.HashPassword(next)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := model.UpdateUserPassword(h.DB, user.ID, hash); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := model.SetMustChangePassword(h.DB, user.ID, false); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.setFlash(w, "Password updated successfully")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
