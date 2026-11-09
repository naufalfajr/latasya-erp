package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
)

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_id"); err == nil {
		if _, err := auth.GetSessionUserID(h.DB, cookie.Value); err == nil {
			http.Redirect(w, r, h.BasePath+"/", http.StatusSeeOther)
			return
		}
	}
	h.render(w, r, "templates/auth/login.html", "Login", map[string]any{
		"Next": safeMCPReturnTo(r.URL.Query().Get("next"), h.BasePath),
	})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.render(w, r, "templates/auth/login.html", "Login", map[string]string{
			"Error":    "Username and password are required",
			"Username": username,
			"Next":     safeMCPReturnTo(r.FormValue("next"), h.BasePath),
		})
		return
	}

	user, err := h.Access.LookupUserForAuth(r.Context(), username)
	if err != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:        "auth.login_failed",
			ActorUsername: username,
			Result:        audit.ResultFail,
			Metadata:      map[string]any{"reason": "unknown_user"},
		})
		h.render(w, r, "templates/auth/login.html", "Login", map[string]string{
			"Error":    "Invalid username or password",
			"Username": username,
			"Next":     safeMCPReturnTo(r.FormValue("next"), h.BasePath),
		})
		return
	}
	if !auth.CheckPassword(user.Password, password) {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:        "auth.login_failed",
			ActorID:       int64(user.ID),
			ActorUsername: username,
			Result:        audit.ResultFail,
			Metadata:      map[string]any{"reason": "bad_password"},
		})
		h.render(w, r, "templates/auth/login.html", "Login", map[string]string{
			"Error":    "Invalid username or password",
			"Username": username,
			"Next":     safeMCPReturnTo(r.FormValue("next"), h.BasePath),
		})
		return
	}

	if !user.IsActive {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:        "auth.login_failed",
			ActorID:       int64(user.ID),
			ActorUsername: username,
			Result:        audit.ResultFail,
			Metadata:      map[string]any{"reason": "inactive"},
		})
		h.render(w, r, "templates/auth/login.html", "Login", map[string]string{
			"Error":    "Account is disabled",
			"Username": username,
			"Next":     safeMCPReturnTo(r.FormValue("next"), h.BasePath),
		})
		return
	}

	// Catch legacy deploys where admin is still using the seeded default.
	if username == "admin" && password == "admin" && !user.MustChangePassword {
		_ = h.Access.SetPasswordChangeRequired(r.Context(), user.ID, true)
		user.MustChangePassword = true
	}

	// Invalidate existing sessions to prevent session fixation
	auth.DeleteUserSessions(h.DB, user.ID)

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
		Secure:   !h.DevMode,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   48 * 60 * 60,
	})

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:        "auth.login",
		ActorID:       int64(user.ID),
		ActorUsername: user.Username,
		TargetType:    "user",
		TargetID:      int64(user.ID),
		TargetLabel:   user.Username,
	})

	if user.MustChangePassword {
		http.Redirect(w, r, h.BasePath+"/password/change", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, safeMCPReturnTo(r.FormValue("next"), h.BasePath), http.StatusSeeOther)
}

// safeMCPReturnTo accepts only this app's MCP consent URL, preserving its
// query (the handoff code). Anything else — other paths, absolute URLs,
// scheme-relative URLs — falls back to the dashboard, so `next` can never
// become an open redirect. Modeled on safeJournalReturnTo.
func safeMCPReturnTo(value, basePath string) string {
	fallback := strings.TrimSuffix(basePath, "/") + "/"
	if value == "" || len(value) > 512 || strings.ContainsAny(value, "\\\r\n") {
		return fallback
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.IsAbs() || parsed.Opaque != "" || parsed.Host != "" || parsed.User != nil ||
		parsed.Fragment != "" {
		return fallback
	}
	path := parsed.Path
	withQuery := func(p string) string {
		if parsed.RawQuery != "" {
			return p + "?" + parsed.RawQuery
		}
		return p
	}
	// /oauth/authorize lives unprefixed on the outer mux: only its bare
	// form is ever valid, in every deployment.
	if path == "/oauth/authorize" {
		return withQuery(path)
	}
	return fallback
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_id"); err == nil {
		// Resolve the actor before deleting the session so the audit row has
		// username attribution. Silent if the session was already invalid.
		if userID, err := auth.GetSessionUserID(h.DB, cookie.Value); err == nil {
			if user, err := h.Access.LookupUserByID(r.Context(), userID); err == nil {
				audit.Log(r.Context(), h.DB, audit.Event{
					Action:        "auth.logout",
					ActorID:       int64(user.ID),
					ActorUsername: user.Username,
					TargetType:    "user",
					TargetID:      int64(user.ID),
					TargetLabel:   user.Username,
				})
			}
		}
		auth.DeleteSession(h.DB, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "session_id",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, h.BasePath+"/login", http.StatusSeeOther)
}
