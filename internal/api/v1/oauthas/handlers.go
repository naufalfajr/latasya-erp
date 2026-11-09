// Package oauthas is the JSON half of the ERP's OAuth 2.1 authorization
// server: the token endpoint (RFC 6749) and the discovery documents (RFC
// 8414 / RFC 9728) that MCP clients use to find them. The consent screen
// lives in internal/handler (HTML surface).
package oauthas

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	v1 "github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/model"
	oauthas "github.com/naufal/latasya-erp/internal/oauthas"
)

type Handler struct {
	OAuth *oauthas.Module
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("POST /oauth/token", v1.OAuthTokenLimiter()(http.HandlerFunc(h.Token)))
	mux.Handle("POST /oauth/register", v1.OAuthTokenLimiter()(http.HandlerFunc(h.Register)))
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", h.ProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", h.AuthorizationServer)
}

// baseURL derives the external origin from the request host (the reverse
// proxy preserves it) using the same scheme logic as the 401 discovery
// header, so the two can never disagree.
func baseURL(r *http.Request) string {
	return v1.ExternalScheme(r) + "://" + r.Host
}

// Token handles POST /oauth/token (grant_type=authorization_code, PKCE).
// Responses follow RFC 6749 §5.2 for errors; tokens are the same lat_
func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	if got := r.PostFormValue("grant_type"); got != "authorization_code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	code := r.PostFormValue("code")
	verifier := r.PostFormValue("code_verifier")
	clientID := r.PostFormValue("client_id")
	redirectURI := r.PostFormValue("redirect_uri")
	if code == "" || verifier == "" || clientID == "" || redirectURI == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, code_verifier, client_id, and redirect_uri are required")
		return
	}

	created, scopes, err := h.OAuth.Exchange(r.Context(), code, verifier, clientID, redirectURI)
	if err != nil {
		switch {
		case errors.Is(err, oauthas.ErrInvalidGrant):
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code is invalid, expired, or already used")
		case errors.Is(err, oauthas.ErrInvalidScope):
			writeOAuthError(w, http.StatusBadRequest, "invalid_scope", "capabilities changed since consent; re-authorize")
		default:
			v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "token exchange failed", nil)
		}
		return
	}

	expiresIn := int64(0)
	if created.Token.ExpiresAt != nil {
		expiresIn = int64(time.Until(*created.Token.ExpiresAt).Seconds())
		if expiresIn < 0 {
			expiresIn = 0
		}
	}
	v1.WriteJSON(w, http.StatusOK, map[string]any{
		"access_token": created.Plaintext,
		"token_type":   "Bearer",
		"scope":        strings.Join(scopes, " "),
		"expires_in":   expiresIn,
	})
}

// Register implements RFC 7591 dynamic client registration: public PKCE
// clients only, and the minted client_id is opaque — never validated later.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "Content-Type must be application/json")
		return
	}
	var meta struct {
		ClientName   string   `json:"client_name"`
		RedirectUris []string `json:"redirect_uris"`
		TokenAuth    string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "malformed JSON body")
		return
	}
	if len(meta.RedirectUris) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	// RFC 6749 §3.1.2: absolute URIs, no fragments.
	for _, raw := range meta.RedirectUris {
		u, err := url.Parse(raw)
		if err != nil || !u.IsAbs() || u.Fragment != "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris entries must be absolute URIs without fragments")
			return
		}
	}
	// Absent token_endpoint_auth_method counts as "none"; the RFC 7591 §2
	// default (client_secret_basic) has no meaning for a secret-less AS.
	if meta.TokenAuth != "" && meta.TokenAuth != "none" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "only public clients (token_endpoint_auth_method=none) are supported")
		return
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		v1.WriteError(w, r, http.StatusInternalServerError, v1.CodeInternal, "registration failed", nil)
		return
	}
	reg := map[string]any{
		"client_id":                  hex.EncodeToString(buf),
		"client_id_issued_at":        time.Now().Unix(),
		"redirect_uris":              meta.RedirectUris,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	if meta.ClientName != "" {
		reg["client_name"] = meta.ClientName
	}
	v1.WriteJSON(w, http.StatusCreated, reg)
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `","error_description":"` + desc + `"}`))
}

// ProtectedResource serves RFC 9728 metadata: how clients discover the AS
// from a 401 on /mcp.
func (h *Handler) ProtectedResource(w http.ResponseWriter, r *http.Request) {
	v1.WriteJSON(w, http.StatusOK, map[string]any{
		"resource":              baseURL(r) + "/mcp",
		"authorization_servers": []string{baseURL(r)},
	})
}

// AuthorizationServer serves RFC 8414 metadata for this first-party AS.
func (h *Handler) AuthorizationServer(w http.ResponseWriter, r *http.Request) {
	base := baseURL(r)
	scopes := append([]string(nil), model.AllCapabilities...)
	v1.WriteJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"grant_types_supported":                 []string{"authorization_code"},
		"response_types_supported":              []string{"code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      scopes,
	})
}
