// Package apitokens implements the API endpoints that let users manage their
// own API tokens (the long-lived Bearer credentials used by bots/MCP/Telegram
// integrations).
//
// All mutating endpoints in this package are COOKIE-ONLY: a request that was
// itself authenticated via a Bearer token cannot create or revoke other
// tokens. This prevents a leaked token from minting fresh tokens or covering
// its own tracks. Listing is permitted on both auth paths since it is
// read-only and never exposes the plaintext.
package apitokens

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

// Handler exposes the api-tokens endpoints. It is wired up against apiMux in
// cmd/server/main.go.
type Handler struct {
	DB *sql.DB
}

// createInput is the JSON shape POST /api-tokens accepts.
type createInput struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// tokenView is the safe serialization of an APIToken: it never includes the
// hash and never includes the plaintext (the plaintext is only ever attached
// once, in the response to Create, via tokenCreatedView).
type tokenView struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type tokenCreatedView struct {
	tokenView
	Plaintext string `json:"plaintext"`
}

func toView(t *model.APIToken) tokenView {
	scopes := t.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return tokenView{
		ID:         t.ID,
		Name:       t.Name,
		Prefix:     t.TokenPrefix,
		Scopes:     scopes,
		ExpiresAt:  t.ExpiresAt,
		LastUsedAt: t.LastUsedAt,
		RevokedAt:  t.RevokedAt,
		CreatedAt:  t.CreatedAt,
	}
}

// validateScopes ensures the requesting user can grant every requested scope.
// Admin can grant any capability. Non-admins are limited to the capabilities
// they currently hold (server-side enforcement — the UI is not trusted).
func validateScopes(requestedScopes []string, user *model.User) (string, bool) {
	if user.Role == model.RoleAdmin {
		return "", true
	}
	userCapSet := make(map[string]bool, len(user.Capabilities))
	for _, c := range user.Capabilities {
		userCapSet[c] = true
	}
	for _, scope := range requestedScopes {
		if !userCapSet[scope] {
			return scope, false
		}
	}
	return "", true
}

// Create handles POST /api/v1/api-tokens. Cookie-only.
//
// On success it returns 201 with the new token's plaintext attached. The
// plaintext is shown ONCE and is unrecoverable afterward; subsequent List
// calls only return the prefix.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if v1.IsBearerAuth(r.Context()) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden,
			"api token cannot create or revoke api tokens", nil)
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	var inp createInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}

	fields := make(map[string]string)
	name := strings.TrimSpace(inp.Name)
	if name == "" {
		fields["name"] = "required"
	}
	if inp.Scopes == nil {
		// scopes is required (subset of caps); empty array is allowed but
		// nil/missing is treated as a validation error to match the OpenAPI
		// schema (`required: [name, scopes]`).
		fields["scopes"] = "required"
	}
	if inp.ExpiresAt != nil && !inp.ExpiresAt.After(time.Now().UTC()) {
		fields["expires_at"] = "must be in the future"
	}
	if len(fields) > 0 {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", fields)
		return
	}

	if badScope, ok := validateScopes(inp.Scopes, user); !ok {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed,
			"scope is not in your capabilities", map[string]string{
				"scopes": "scope " + strconv.Quote(badScope) + " is not in your capabilities",
			})
		return
	}

	token, plaintext, err := model.CreateAPIToken(h.DB, user.ID, name, inp.Scopes, inp.ExpiresAt)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to create api token", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "api_token.create",
		TargetType:  "api_token",
		TargetID:    int64(token.ID),
		TargetLabel: token.Name,
		Metadata: map[string]any{
			"name":       token.Name,
			"scopes":     token.Scopes,
			"expires_at": token.ExpiresAt,
		},
	})

	view := tokenCreatedView{
		tokenView: toView(token),
		Plaintext: plaintext,
	}
	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": view})
}

// List handles GET /api/v1/api-tokens. Allowed under both cookie and bearer
// auth (read-only; never includes plaintext or hash). Not paginated: a single
// user typically has only a handful of tokens.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	tokens, err := model.ListAPITokensByUser(h.DB, user.ID)
	if err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to list api tokens", nil)
		return
	}

	views := make([]tokenView, 0, len(tokens))
	for i := range tokens {
		views = append(views, toView(&tokens[i]))
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": views})
}

// Revoke handles DELETE /api/v1/api-tokens/{id}. Cookie-only.
//
// model.RevokeAPIToken scopes the UPDATE by user_id, so a user cannot revoke
// another user's token even by guessing IDs — they get 404.
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	if v1.IsBearerAuth(r.Context()) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden,
			"api token cannot create or revoke api tokens", nil)
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid token id", nil)
		return
	}

	// Look up first so we can record a useful audit label. Scoped to this
	// user so cross-user revocation attempts surface as 404.
	var name string
	row := h.DB.QueryRow(`SELECT name FROM api_tokens WHERE id = ? AND user_id = ?`, id, user.ID)
	if err := row.Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "api token not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to lookup api token", nil)
		return
	}

	if err := model.RevokeAPIToken(h.DB, user.ID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "api token not found", nil)
			return
		}
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to revoke api token", nil)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:      "api_token.revoke",
		TargetType:  "api_token",
		TargetID:    int64(id),
		TargetLabel: name,
		Metadata: map[string]any{
			"token_id": id,
			"name":     name,
		},
	})

	w.WriteHeader(http.StatusNoContent)
}
