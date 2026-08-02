// Package apitokens exposes self-service bearer credential management.
package apitokens

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/apitoken"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type Handler struct{ Tokens *apitoken.Module }

type createInput struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
}

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

func toView(token *model.APIToken) tokenView {
	scopes := token.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return tokenView{ID: token.ID, Name: token.Name, Prefix: token.TokenPrefix, Scopes: scopes, ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt, RevokedAt: token.RevokedAt, CreatedAt: token.CreatedAt}
}

func tokenActor(r *http.Request) apitoken.Actor {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		return apitoken.Actor{}
	}
	return apitoken.Actor{UserID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin(), Capabilities: u.Capabilities}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, mutation func(http.Handler) http.Handler) {
	mux.HandleFunc("GET /api/v1/api-tokens", h.List)
	mux.Handle("POST /api/v1/api-tokens", mutation(http.HandlerFunc(h.Create)))
	mux.HandleFunc("DELETE /api/v1/api-tokens/{id}", h.Revoke)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if v1.IsBearerAuth(r.Context()) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "api token cannot create or revoke api tokens", nil)
		return
	}
	if auth.UserFromContext(r.Context()) == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}
	var inp createInput
	if err := v1.DecodeJSON(w, r, &inp); err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid request body", nil)
		return
	}
	if inp.Scopes == nil {
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", map[string]string{"scopes": "required"})
		return
	}
	created, err := h.Tokens.Create(r.Context(), tokenActor(r), apitoken.Draft{Name: inp.Name, Scopes: inp.Scopes, ExpiresAt: inp.ExpiresAt})
	if err != nil {
		writeTokenError(w, r, err)
		return
	}
	v1.WriteJSON(w, http.StatusCreated, map[string]any{"data": tokenCreatedView{tokenView: toView(created.Token), Plaintext: created.Plaintext}})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	if u == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}
	tokens, err := h.Tokens.List(r.Context(), tokenActor(r))
	if err != nil {
		writeTokenError(w, r, err)
		return
	}
	views := make([]tokenView, 0, len(tokens))
	for i := range tokens {
		views = append(views, toView(&tokens[i]))
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{"data": views})
}

func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	if v1.IsBearerAuth(r.Context()) {
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "api token cannot create or revoke api tokens", nil)
		return
	}
	if auth.UserFromContext(r.Context()) == nil {
		v1.WriteError(w, r, http.StatusUnauthorized, v1.CodeUnauthorized, "authentication required", nil)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		v1.WriteError(w, r, http.StatusBadRequest, v1.CodeInvalidRequest, "invalid token id", nil)
		return
	}
	if _, err := h.Tokens.Revoke(r.Context(), tokenActor(r), id); err != nil {
		writeTokenError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeTokenError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *apitoken.ValidationError
	var conflict *apitoken.ConflictError
	switch {
	case errors.Is(err, apitoken.ErrNotFound):
		v1.WriteError(w, r, http.StatusNotFound, v1.CodeNotFound, "api token not found", nil)
	case errors.Is(err, apitoken.ErrForbidden):
		v1.WriteError(w, r, http.StatusForbidden, v1.CodeForbidden, "insufficient permissions", nil)
	case errors.As(err, &validation):
		v1.WriteError(w, r, http.StatusUnprocessableEntity, v1.CodeValidationFailed, "validation failed", validation.Fields)
	case errors.As(err, &conflict):
		v1.WriteError(w, r, http.StatusConflict, v1.CodeConflict, conflict.Message, nil)
	default:
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "failed to manage api token", nil)
	}
}
