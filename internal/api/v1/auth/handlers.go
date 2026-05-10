// Package auth provides JSON API endpoints for the Latasya ERP authentication
// flow: login, logout, me, csrf, and password-change. Cookie sessions and
// Bearer tokens are both honoured by the upstream BearerOrCookie middleware;
// these handlers add the JSON envelope and the audit logging that the legacy
// HTML handlers already do.
package auth

import (
	"database/sql"
	"net/http"

	"github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// Handler bundles the dependencies needed by every auth endpoint.
type Handler struct {
	DB      *sql.DB
	DevMode bool
}

// New constructs a Handler.
func New(db *sql.DB, devMode bool) *Handler {
	return &Handler{DB: db, DevMode: devMode}
}

const sessionCookieMaxAgeSeconds = 48 * 60 * 60

// userPayload is the public-facing snapshot of model.User returned in JSON
// envelopes. Capabilities is always non-nil so JSON serializes [] not null.
type userPayload struct {
	ID                 int      `json:"id"`
	Username           string   `json:"username"`
	FullName           string   `json:"full_name"`
	Role               string   `json:"role"`
	Capabilities       []string `json:"capabilities"`
	MustChangePassword bool     `json:"must_change_password"`
}

func toUserPayload(u *model.User) userPayload {
	caps := u.Capabilities
	if u.Role == model.RoleAdmin {
		// Admin role implicitly holds every capability; expose the canonical
		// list so clients don't have to special-case role=admin.
		caps = model.AllCapabilities
	}
	if caps == nil {
		caps = []string{}
	}
	return userPayload{
		ID:                 u.ID,
		Username:           u.Username,
		FullName:           u.FullName,
		Role:               u.Role,
		Capabilities:       caps,
		MustChangePassword: u.MustChangePassword,
	}
}

// loginRequest is the JSON body accepted by POST /auth/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login authenticates a user via JSON credentials, creates a session, and
// returns the session cookie plus a CSRF token for subsequent cookie-based
// state-changing requests.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := v1.DecodeJSON(w, r, &req); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid JSON body", nil)
		return
	}

	fields := map[string]string{}
	if req.Username == "" {
		fields["username"] = "required"
	}
	if req.Password == "" {
		fields["password"] = "required"
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "username and password are required", fields)
		return
	}

	user, err := model.GetUserByUsername(h.DB, req.Username)
	if err != nil {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:        "auth.login_failed",
			ActorUsername: req.Username,
			Result:        audit.ResultFail,
			Metadata:      map[string]any{"reason": "unknown_user"},
		})
		v1.WriteError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid username or password", nil)
		return
	}
	if !auth.CheckPassword(user.Password, req.Password) {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:        "auth.login_failed",
			ActorID:       int64(user.ID),
			ActorUsername: req.Username,
			Result:        audit.ResultFail,
			Metadata:      map[string]any{"reason": "bad_password"},
		})
		v1.WriteError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid username or password", nil)
		return
	}
	if !user.IsActive {
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:        "auth.login_failed",
			ActorID:       int64(user.ID),
			ActorUsername: req.Username,
			Result:        audit.ResultFail,
			Metadata:      map[string]any{"reason": "inactive"},
		})
		v1.WriteError(w, r, http.StatusUnauthorized, "invalid_credentials", "account is disabled", nil)
		return
	}

	// Mirror the HTML handler's catch for the seeded admin/admin default so
	// JSON-only clients can't sidestep the forced rotation.
	if req.Username == "admin" && req.Password == "admin" && !user.MustChangePassword {
		_ = model.SetMustChangePassword(h.DB, user.ID, true)
		user.MustChangePassword = true
	}

	// Session fixation prevention: drop any prior sessions for this user
	// before issuing a fresh one.
	_ = auth.DeleteUserSessions(h.DB, user.ID)

	sessionID, err := auth.CreateSession(h.DB, user.ID)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create session", nil)
		return
	}
	csrfToken, err := auth.GetSessionCSRF(h.DB, sessionID)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to read csrf token", nil)
		return
	}

	// Re-load capabilities for non-admins so the response payload reflects
	// what the new session actually carries (admin short-circuits).
	if user.Role != model.RoleAdmin {
		if role, err := model.GetRoleByName(h.DB, user.Role); err == nil {
			user.Capabilities = role.Capabilities
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   !h.DevMode,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionCookieMaxAgeSeconds,
	})

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:        "auth.login",
		ActorID:       int64(user.ID),
		ActorUsername: user.Username,
		TargetType:    "user",
		TargetID:      int64(user.ID),
		TargetLabel:   user.Username,
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"user":       toUserPayload(user),
			"csrf_token": csrfToken,
		},
	})
}

// Logout destroys the current session (cookie path) or no-ops for Bearer
// callers, then clears the session cookie. Always returns 200.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session_id"); err == nil && cookie.Value != "" {
		// Resolve the actor before deleting the session so the audit row
		// has username attribution.
		if userID, err := auth.GetSessionUserID(h.DB, cookie.Value); err == nil {
			if user, err := model.GetUserByID(h.DB, userID); err == nil {
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
		_ = auth.DeleteSession(h.DB, cookie.Value)
	} else if u := auth.UserFromContext(r.Context()); u != nil && v1.IsBearerAuth(r.Context()) {
		// Bearer logout has no session to destroy; record the attempt for
		// audit symmetry.
		audit.Log(r.Context(), h.DB, audit.Event{
			Action:        "auth.logout",
			ActorID:       int64(u.ID),
			ActorUsername: u.Username,
			TargetType:    "user",
			TargetID:      int64(u.ID),
			TargetLabel:   u.Username,
			Metadata:      map[string]any{"auth_method": "bearer"},
		})
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "session_id",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// Me returns the authenticated user's identity plus the auth method used.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	authMethod := "cookie"
	if v1.IsBearerAuth(r.Context()) {
		authMethod = "bearer"
	}

	payload := map[string]any{
		"id":                   user.ID,
		"username":             user.Username,
		"full_name":            user.FullName,
		"role":                 user.Role,
		"capabilities":         toUserPayload(user).Capabilities,
		"must_change_password": user.MustChangePassword,
		"auth_method":          authMethod,
		"token_id":             v1.TokenIDFromContext(r.Context()),
	}

	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": payload})
}

// CSRF returns the CSRF token bound to the current cookie session. Bearer
// callers receive 400, since Bearer-authenticated requests don't participate
// in CSRF validation.
func (h *Handler) CSRF(w http.ResponseWriter, r *http.Request) {
	if v1.IsBearerAuth(r.Context()) {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest,
			"csrf tokens are not used with bearer authentication", nil)
		return
	}
	token := auth.CSRFFromContext(r.Context())
	if token == "" {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"csrf_token": token})
}

// passwordChangeRequest is the JSON body accepted by POST /auth/password/change.
type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

const minPasswordLength = 8

// PasswordChange rotates the authenticated user's password. Allowed for both
// cookie- and bearer-authenticated callers; bearer tokens are not blocked by
// must_change_password since possession of a token implies explicit agency.
func (h *Handler) PasswordChange(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	var req passwordChangeRequest
	if err := v1.DecodeJSON(w, r, &req); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid JSON body", nil)
		return
	}

	fields := map[string]string{}
	if req.CurrentPassword == "" {
		fields["current_password"] = "required"
	}
	if !auth.CheckPassword(user.Password, req.CurrentPassword) && req.CurrentPassword != "" {
		fields["current_password"] = "incorrect"
	}
	if len(req.NewPassword) < minPasswordLength {
		fields["new_password"] = "must be at least 8 characters"
	}
	if req.NewPassword != req.ConfirmPassword {
		fields["confirm_password"] = "does not match new_password"
	}
	if req.NewPassword != "" && req.NewPassword == req.CurrentPassword {
		fields["new_password"] = "must be different from current_password"
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to hash password", nil)
		return
	}
	if err := model.UpdateUserPassword(h.DB, user.ID, hash); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to update password", nil)
		return
	}
	if err := model.SetMustChangePassword(h.DB, user.ID, false); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to clear must_change_password", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "auth.password_change",
		ActorID:     int64(user.ID),
		TargetType:  "user",
		TargetID:    int64(user.ID),
		TargetLabel: user.Username,
		Metadata: map[string]any{
			"forced":      user.MustChangePassword,
			"auth_method": authMethodFromCtx(r),
		},
	})

	v1.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// authMethodFromCtx is a small helper for audit metadata.
func authMethodFromCtx(r *http.Request) string {
	if v1.IsBearerAuth(r.Context()) {
		return "bearer"
	}
	return "cookie"
}

