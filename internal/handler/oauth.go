package handler

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"

	"github.com/naufal/latasya-erp/internal/api/v1"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/oauthas"
)

// remote MCP client. The authorize page lives on the outer mux so it can
// bounce unauthenticated users through login and back (safeMCPReturnTo
// allows exactly this path).
func (h *Handler) RegisterOAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /oauth/authorize", h.OAuthAuthorizePage)
	mux.HandleFunc("POST /oauth/authorize", h.OAuthAuthorizeSubmit)
}

// validRedirectURI accepts loopback redirect targets (native MCP clients,
// RFC 8252 — any port) and same-origin https/http targets.
func validRedirectURI(raw string, r *http.Request) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	// ::1 is excluded: CSP host-sources cannot express it, so a consent
	// redirect there would be browser-blocked anyway. "localhost" covers it.
	if u.Scheme == "http" && (host == "127.0.0.1" || host == "localhost") {
		return true
	}
	sameOrigin := u.Scheme+"//"+u.Host == v1.ExternalScheme(r)+"//"+r.Host
	return sameOrigin && (u.Scheme == "https" || u.Scheme == "http")
}

// OAuthAuthorizePage renders the OAuth consent screen. Unauthenticated users
// are sent to login with next= so the client's authorize URL survives.
func (h *Handler) OAuthAuthorizePage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	user, session, ok := h.oauthSession(w, r)
	if !ok {
		return
	}

	data := map[string]any{
		"ClientID":        q.Get("client_id"),
		"RedirectURI":     q.Get("redirect_uri"),
		"State":           q.Get("state"),
		"CodeChallenge":   q.Get("code_challenge"),
		"CSRFToken":       session.CSRFToken,
		"Checked":         checkedScopes(q.Get("scope"), user),
		"AvailableScopes": availableScopes(user),
		"Errors":          map[string]string{},
	}
	if msg := h.oauthParamError(q, r); msg != "" {
		data["Errors"].(map[string]string)["general"] = msg
	}
	h.render(w, r, "templates/mcp/oauth_consent.html", "Authorize MCP", data)
}

// oauthParamError validates the authorize request shape; empty means valid.
func (h *Handler) oauthParamError(q url.Values, r *http.Request) string {
	if q.Get("response_type") != "code" {
		return "Only response_type=code is supported."
	}
	if strings.TrimSpace(q.Get("client_id")) == "" || len(q.Get("client_id")) > 128 {
		return "A client_id is required."
	}
	if !validRedirectURI(q.Get("redirect_uri"), r) {
		return "redirect_uri must be a loopback or same-origin URL."
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		return "PKCE with code_challenge_method=S256 is required."
	}
	return ""
}
func checkedScopes(raw string, user *model.User) map[string]bool {
	allowed := availableScopes(user)
	held := make(map[string]bool, len(allowed))
	for _, s := range allowed {
		held[s] = true
	}
	requested := strings.Fields(raw)
	checked := make(map[string]bool, len(allowed))
	if len(requested) == 0 {
		for s := range held {
			checked[s] = true
		}
		return checked
	}
	for _, s := range requested {
		if held[s] {
			checked[s] = true
		}
	}
	if len(checked) == 0 {
		for s := range held {
			checked[s] = true
		}
	}
	return checked
}

func allScopesChecked(user *model.User) map[string]bool {
	checked := map[string]bool{}
	for _, s := range availableScopes(user) {
		checked[s] = true
	}
	return checked
}

// OAuthAuthorizeSubmit records the consent and redirects with the code (or
func (h *Handler) OAuthAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	user, session, ok := h.oauthSession(w, r)
	if !ok {
		return
	}
	// This route sits on the outer mux (no CSRFProtect chain), so the token
	// is compared here, constant-time like the HTML surface.
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf_token")), []byte(session.CSRFToken)) != 1 {
		http.Error(w, "Forbidden: invalid CSRF token", http.StatusForbidden)
		return
	}

	// The deny redirect must never target an unvalidated URI: validate the
	// authorize params first and render an error page when they are invalid.
	if msg := h.oauthParamError(q, r); msg != "" {
		h.render(w, r, "templates/mcp/oauth_consent.html", "Authorize MCP", map[string]any{
			"ClientID":        q.Get("client_id"),
			"RedirectURI":     q.Get("redirect_uri"),
			"State":           q.Get("state"),
			"CodeChallenge":   q.Get("code_challenge"),
			"CSRFToken":       session.CSRFToken,
			"Checked":         allScopesChecked(user),
			"AvailableScopes": availableScopes(user),
			"Errors":          map[string]string{"general": msg},
		})
		return
	}
	redirectWith := func(extra url.Values) string {
		// Merge into the redirect URI's existing query instead of blindly
		// appending "?": loopback callbacks may already carry parameters.
		u, err := url.Parse(q.Get("redirect_uri"))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return q.Get("redirect_uri") // params were validated earlier
		}
		params := u.Query()
		for k, v := range extra {
			params.Set(k, v[0])
		}
		u.RawQuery = params.Encode()
		return u.String()
	}
	fail := func(errCode string) {
		extra := url.Values{"error": {errCode}}
		if st := q.Get("state"); st != "" {
			extra.Set("state", st)
		}
		http.Redirect(w, r, redirectWith(extra), http.StatusSeeOther)
	}

	if r.PostFormValue("action") == "deny" {
		fail("access_denied")
		return
	}

	// Scopes are validated against the CURRENT user's capabilities here and
	// re-derived at exchange time.
	allowed := availableScopes(user)
	held := make(map[string]bool, len(allowed))
	for _, s := range allowed {
		held[s] = true
	}
	chosen := map[string]bool{}
	var scopes []string
	for _, s := range r.PostForm["scopes"] {
		if held[s] && !chosen[s] {
			scopes = append(scopes, s)
			chosen[s] = true
		}
	}
	if len(scopes) == 0 {
		h.render(w, r, "templates/mcp/oauth_consent.html", "Authorize MCP", map[string]any{
			"ClientID":        q.Get("client_id"),
			"RedirectURI":     q.Get("redirect_uri"),
			"State":           q.Get("state"),
			"CodeChallenge":   q.Get("code_challenge"),
			"CSRFToken":       session.CSRFToken,
			"Checked":         allScopesChecked(user),
			"AvailableScopes": availableScopes(user),
			"Errors":          map[string]string{"scopes": "Select at least one capability."},
		})
		return
	}

	code, err := h.OAuth.Issue(r.Context(), oauthas.Grant{
		UserID:        user.ID,
		Username:      user.Username,
		ClientID:      q.Get("client_id"),
		RedirectURI:   q.Get("redirect_uri"),
		CodeChallenge: q.Get("code_challenge"),
		Scopes:        scopes,
	})
	if err != nil {
		fail("server_error")
		return
	}
	audit.Log(r.Context(), h.DB, audit.Event{
		Action:        "mcp.oauth.consent",
		ActorID:       int64(user.ID),
		ActorUsername: user.Username,
		TargetType:    "oauth_client",
		TargetLabel:   q.Get("client_id"),
		Metadata:      map[string]any{"scopes": scopes},
	})

	extra := url.Values{"code": {code}}
	if st := q.Get("state"); st != "" {
		extra.Set("state", st)
	}
	http.Redirect(w, r, redirectWith(extra), http.StatusSeeOther)
}

// oauthSession resolves the browser session, redirecting to login with a
// bounce-back next URL when absent.
func (h *Handler) oauthSession(w http.ResponseWriter, r *http.Request) (*model.User, *auth.SessionInfo, bool) {
	cookie, err := r.Cookie("session_id")
	if err == nil {
		if session, serr := auth.GetSession(h.DB, cookie.Value); serr == nil {
			user, uerr := h.Access.LookupUserByIDForAuth(r.Context(), session.UserID)
			if uerr == nil && user.IsActive {
				// This page sits on the outer mux, so RequireAuth's
				// role-capability loading never runs — without it every
				// non-admin would see an empty scope grid (the user row
				// alone carries no capabilities).
				if user.Role != model.RoleAdmin {
					if role, rerr := h.Access.LookupRoleForAuth(r.Context(), user.Role); rerr == nil {
						user.Capabilities = role.Capabilities
					}
				}
				return user, session, true
			}
		}
	}
	http.Redirect(w, r, h.BasePath+"/login?next="+url.QueryEscape(safeMCPReturnTo(r.URL.RequestURI(), h.BasePath)), http.StatusSeeOther)
	return nil, nil, false
}
