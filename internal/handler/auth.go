package handler

import (
	"net/http"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_id"); err == nil {
		if _, err := auth.GetSessionUserID(h.DB, cookie.Value); err == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	h.render(w, r, "templates/auth/login.html", "Login", nil)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.render(w, r, "templates/auth/login.html", "Login", map[string]string{
			"Error":    "Username and password are required",
			"Username": username,
		})
		return
	}

	user, err := model.GetUserByUsername(h.DB, username)
	if err != nil {
		h.render(w, r, "templates/auth/login.html", "Login", map[string]string{
			"Error":    "Invalid username or password",
			"Username": username,
		})
		return
	}
	if !auth.CheckPassword(user.Password, password) {
		h.render(w, r, "templates/auth/login.html", "Login", map[string]string{
			"Error":    "Invalid username or password",
			"Username": username,
		})
		return
	}

	if !user.IsActive {
		h.render(w, r, "templates/auth/login.html", "Login", map[string]string{
			"Error":    "Account is disabled",
			"Username": username,
		})
		return
	}

	sessionID, err := auth.CreateSession(h.DB, user.ID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 60 * 60,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_id"); err == nil {
		auth.DeleteSession(h.DB, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "session_id",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
