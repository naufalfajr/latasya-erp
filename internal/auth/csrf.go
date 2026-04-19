package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
)

const csrfContextKey contextKey = "csrf_token"

// generateCSRFToken returns a cryptographically-random 64-char hex token.
func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate csrf token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CSRFFromContext returns the CSRF token attached to the request context,
// or empty string if none.
func CSRFFromContext(ctx context.Context) string {
	t, _ := ctx.Value(csrfContextKey).(string)
	return t
}

// withCSRF returns a new context carrying the given CSRF token.
func withCSRF(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfContextKey, token)
}

// CSRFProtect rejects state-changing requests whose CSRF token does not
// match the one stored on the current session. Safe methods pass through.
// The token is accepted either as form field "csrf_token" or header
// "X-CSRF-Token" (for HTMX / fetch requests).
//
// Must be wrapped INSIDE RequireAuth so that the session CSRF has already
// been attached to the request context.
func CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		expected := CSRFFromContext(r.Context())
		if expected == "" {
			http.Error(w, "Forbidden: missing CSRF token", http.StatusForbidden)
			return
		}
		supplied := r.Header.Get("X-CSRF-Token")
		if supplied == "" {
			// ParseForm is idempotent; handlers later call FormValue safely.
			_ = r.ParseForm()
			supplied = r.PostFormValue("csrf_token")
		}
		if len(supplied) != len(expected) ||
			subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) != 1 {
			http.Error(w, "Forbidden: invalid CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// GetSessionCSRF returns the CSRF token bound to the given session, if the
// session is still valid.
func GetSessionCSRF(db *sql.DB, sessionID string) (string, error) {
	var token string
	err := db.QueryRow(
		"SELECT csrf_token FROM sessions WHERE id = ? AND expires_at > datetime('now')",
		sessionID,
	).Scan(&token)
	if err != nil {
		return "", fmt.Errorf("get session csrf: %w", err)
	}
	return token, nil
}
