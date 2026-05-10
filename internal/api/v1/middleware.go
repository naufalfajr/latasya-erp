package v1

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type contextKey string

const (
	tokenIDKey       contextKey = "token_id"
	effectiveCapsKey contextKey = "effective_caps"
)

// BearerOrCookie authenticates requests via either an Authorization: Bearer
// token or an existing session cookie. Bearer takes precedence when both are
// present. On the Bearer path, capabilities are intersected at request time
// between the token's stored scopes and the user's current role capabilities,
// so revoking a role capability immediately reduces all of that user's tokens.
//
// Cookie path delegates to auth.RequireAuth (which redirects to /login on
// failure). Bearer path returns JSON 401 envelopes. Requests with neither
// credential type return JSON 401.
func BearerOrCookie(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			_, cookieErr := r.Cookie("session_id")
			hasCookie := cookieErr == nil

			if strings.HasPrefix(authHeader, "Bearer ") {
				if hasCookie {
					slog.Warn("api: both bearer and cookie present, preferring bearer",
						"path", r.URL.Path)
				}
				plaintext := strings.TrimPrefix(authHeader, "Bearer ")
				if plaintext == "" {
					WriteError(w, r, http.StatusUnauthorized, CodeInvalidToken, "invalid or expired token", nil)
					return
				}
				h := sha256.Sum256([]byte(plaintext))
				hash := hex.EncodeToString(h[:])

				token, err := model.GetAPITokenByHash(db, hash)
				if err != nil {
					WriteError(w, r, http.StatusUnauthorized, CodeInvalidToken, "invalid or expired token", nil)
					return
				}

				user, err := model.GetUserByID(db, token.UserID)
				if err != nil || !user.IsActive {
					WriteError(w, r, http.StatusUnauthorized, CodeInvalidToken, "token user not found or inactive", nil)
					return
				}

				if user.Role != model.RoleAdmin {
					if role, err := model.GetRoleByName(db, user.Role); err == nil {
						user.Capabilities = role.Capabilities
					}
				}

				effectiveCaps := intersectScopes(token.Scopes, user.Capabilities, user.Role == model.RoleAdmin)

				ctx := auth.WithUser(r.Context(), user)
				ctx = auth.MarkBearerAuth(ctx)
				ctx = context.WithValue(ctx, tokenIDKey, token.ID)
				ctx = audit.WithTokenID(ctx, token.ID)
				ctx = context.WithValue(ctx, effectiveCapsKey, effectiveCaps)

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if hasCookie {
				auth.RequireAuth(db, next).ServeHTTP(w, r)
				return
			}

			WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "authentication required", nil)
		})
	}
}

// intersectScopes returns the subset of tokenScopes that the user currently
// holds. Admin users bypass intersection: their tokens carry whatever scopes
// were issued, since admin implicitly holds every capability.
func intersectScopes(tokenScopes, userCaps []string, isAdmin bool) []string {
	if isAdmin {
		return tokenScopes
	}
	capSet := make(map[string]bool, len(userCaps))
	for _, c := range userCaps {
		capSet[c] = true
	}
	result := make([]string, 0, len(tokenScopes))
	for _, s := range tokenScopes {
		if capSet[s] {
			result = append(result, s)
		}
	}
	return result
}

// IsBearerAuth reports whether the request was authenticated via Bearer token.
func IsBearerAuth(ctx context.Context) bool {
	return auth.IsBearerAuth(ctx)
}

// TokenIDFromContext returns the API token ID if Bearer auth was used,
// or nil for cookie-authenticated or anonymous requests.
func TokenIDFromContext(ctx context.Context) *int {
	if v, ok := ctx.Value(tokenIDKey).(int); ok {
		return &v
	}
	return nil
}

// EffectiveCapabilitiesFromContext returns the capabilities the request can
// exercise. For Bearer auth, this is the intersection of token scopes and
// user role capabilities. For cookie auth, it falls back to the user's full
// role capabilities.
func EffectiveCapabilitiesFromContext(ctx context.Context) []string {
	if caps, ok := ctx.Value(effectiveCapsKey).([]string); ok {
		return caps
	}
	if u := auth.UserFromContext(ctx); u != nil {
		return u.Capabilities
	}
	return nil
}

// HasEffectiveCapability checks whether the request carries a specific
// capability. For cookie-auth admin users, all capabilities are granted.
// For Bearer-auth requests (including admin-owned tokens), the effective
// capability set (token scopes ∩ user capabilities) is always checked —
// this enforces scope intersection at request time and prevents zombie
// privilege escalation even for admin-owned tokens with limited scopes.
func HasEffectiveCapability(ctx context.Context, cap string) bool {
	if !IsBearerAuth(ctx) {
		if u := auth.UserFromContext(ctx); u != nil && u.Role == model.RoleAdmin {
			return true
		}
	}
	for _, c := range EffectiveCapabilitiesFromContext(ctx) {
		if c == cap {
			return true
		}
	}
	return false
}
