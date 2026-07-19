package auth

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/naufal/latasya-erp/internal/model"
)

type contextKey string

const userContextKey contextKey = "user"

// loginPath is where RequireAuth sends unauthenticated requests. Defaults to
// "/login" (what every test expects); production overrides it once at
// startup via SetLoginPath since the admin app is mounted under a prefix.
var loginPath = "/login"

// SetLoginPath overrides the redirect target used by RequireAuth. Call once
// at startup, before serving traffic.
func SetLoginPath(p string) { loginPath = p }

func UserFromContext(ctx context.Context) *model.User {
	u, _ := ctx.Value(userContextKey).(*model.User)
	return u
}

// WithUser returns a new context carrying the given authenticated user.
// Used by alternate auth middlewares (e.g. Bearer-token) that bypass the
// session-cookie path.
func WithUser(ctx context.Context, u *model.User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

func RequireAuth(db *sql.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Redirect(w, r, loginPath, http.StatusSeeOther)
			return
		}

		session, err := GetSession(db, cookie.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{
				Name:   "session_id",
				Value:  "",
				Path:   "/",
				MaxAge: -1,
			})
			http.Redirect(w, r, loginPath, http.StatusSeeOther)
			return
		}

		user, err := model.GetUserByID(db, session.UserID)
		if err != nil || !user.IsActive {
			http.Redirect(w, r, loginPath, http.StatusSeeOther)
			return
		}

		if ShouldRefresh(session) {
			_ = TouchSession(db, cookie.Value)
		}

		// Load role capabilities for non-admin users; admin's HasCapability
		// short-circuits so we skip the query.
		if user.Role != model.RoleAdmin {
			if role, err := model.GetRoleByName(db, user.Role); err == nil {
				user.Capabilities = role.Capabilities
			}
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		ctx = withCSRF(ctx, session.CSRFToken)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || !user.IsAdmin() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AdminOnly wraps a HandlerFunc with admin authorization check.
func AdminOnly(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || !user.IsAdmin() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		fn(w, r)
	}
}

// RequireCapability wraps a handler with a capability check. The request is
// rejected with 403 if the authenticated user's role does not grant `cap`.
// Admin users implicitly pass every check.
func RequireCapability(cap string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if !user.HasCapability(cap) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CapabilityOnly wraps a HandlerFunc with a capability check. Same semantics
// as RequireCapability but targets a single endpoint.
func CapabilityOnly(cap string, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if !user.HasCapability(cap) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		fn(w, r)
	}
}
