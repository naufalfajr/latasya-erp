package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
)

// mcpExpiryChoices are the consent form's token-lifetime options, in days.
// The empty string means no expiry, mirroring the API-token form.
var mcpExpiryChoices = map[string]int{"30": 30, "90": 90, "": 0}

// RegisterMCPRoutes installs the MCP device-login consent page inside the
// protected mux, so it inherits session auth, CSRF, and the password-change
// gate from the standard chain.
func (h *Handler) RegisterMCPRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /mcp/authorize", h.MCPAuthorizePage)
	mux.HandleFunc("POST /mcp/authorize", h.MCPAuthorizeSubmit)
}

// MCPAuthorizePage renders the consent screen for a pending device login.
func (h *Handler) MCPAuthorizePage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	code := strings.TrimSpace(r.URL.Query().Get("code"))

	handoff, err := h.MCPAuth.Lookup(r.Context(), code)
	if err != nil || handoff.Status != "pending" {
		h.setFlash(w, "Unknown or expired MCP login code.")
		http.Redirect(w, r, h.BasePath+"/", http.StatusSeeOther)
		return
	}

	h.render(w, r, "templates/mcp/authorize.html", "Authorize MCP", map[string]any{
		"Code":            code,
		"Client":          handoff.Client,
		"AvailableScopes": availableScopes(user),
		"Errors":          map[string]string{},
	})
}

// MCPAuthorizeSubmit records the approve/deny decision. No token is minted
// here: the client's next poll does that, with capabilities re-derived then.
func (h *Handler) MCPAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	code := strings.TrimSpace(r.PostFormValue("code"))

	if r.PostFormValue("action") == "deny" {
		if err := h.MCPAuth.Deny(r.Context(), code); err != nil {
			h.setFlash(w, "Unknown or expired MCP login code.")
		} else {
			audit.Log(r.Context(), h.DB, audit.Event{
				Action:        "mcp.handoff.deny",
				ActorID:       int64(user.ID),
				ActorUsername: user.Username,
				TargetType:    "mcp_handoff",
				TargetLabel:   code,
			})
			h.setFlash(w, "MCP login denied.")
		}
		http.Redirect(w, r, h.BasePath+"/", http.StatusSeeOther)
		return
	}

	// Scopes are validated against the CURRENT user's capabilities here and
	// re-validated at mint time; a stale form can never grant more.
	allowed := availableScopes(user)
	allowedSet := make(map[string]bool, len(allowed))
	for _, s := range allowed {
		allowedSet[s] = true
	}
	var scopes []string
	chosen := make(map[string]bool)
	for _, s := range r.PostForm["scopes"] {
		if allowedSet[s] && !chosen[s] {
			scopes = append(scopes, s)
			chosen[s] = true
		}
	}
	if len(scopes) == 0 {
		h.render(w, r, "templates/mcp/authorize.html", "Authorize MCP", map[string]any{
			"Code":            code,
			"Client":          r.PostFormValue("client"),
			"AvailableScopes": allowed,
			"Errors":          map[string]string{"scopes": "Select at least one capability."},
		})
		return
	}

	days, ok := mcpExpiryChoices[r.PostFormValue("expiry_days")]
	if !ok {
		days = 90 // invalid submissions get the default, not an error
	}
	var tokenExpiresAt *time.Time
	if days > 0 {
		t := time.Now().UTC().AddDate(0, 0, days)
		tokenExpiresAt = &t
	}

	if err := h.MCPAuth.Approve(r.Context(), code, user.ID, user.Username, scopes, tokenExpiresAt); err != nil {
		h.setFlash(w, "Unknown or expired MCP login code.")
		http.Redirect(w, r, h.BasePath+"/", http.StatusSeeOther)
		return
	}

	audit.Log(r.Context(), h.DB, audit.Event{
		Action:        "mcp.handoff.approve",
		ActorID:       int64(user.ID),
		ActorUsername: user.Username,
		TargetType:    "mcp_handoff",
		TargetLabel:   code,
		Metadata:      map[string]any{"scopes": scopes, "expires_days": days},
	})
	h.setFlash(w, "Approved. Return to your terminal — latasya-mcp will finish connecting.")
	http.Redirect(w, r, h.BasePath+"/", http.StatusSeeOther)
}
