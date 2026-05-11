package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type apiTokenFormData struct {
	Tokens          []model.APIToken
	AvailableScopes []string
	SelectedScopes  map[string]bool
	Errors          map[string]string
	Token           string
	Name            string
	ExpiresAt       string
}

// IsScopeChecked reports whether a scope was selected (used by templates for
// repopulating checkboxes after a validation error).
func (d apiTokenFormData) IsScopeChecked(scope string) bool {
	return d.SelectedScopes[scope]
}

func availableScopes(user *model.User) []string {
	if user.IsAdmin() {
		return model.AllCapabilities
	}
	return user.Capabilities
}

// consumeFlash reads the "flash" cookie, clears it via Set-Cookie, and rewrites
// the request's Cookie header to remove it so subsequent reads in this request
// (e.g. inside h.render) return empty. Returns the cookie value or "" if absent.
func (h *Handler) consumeFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("flash")
	if err != nil {
		return ""
	}
	// Clear cookie for future requests
	http.SetCookie(w, &http.Cookie{
		Name:   "flash",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	// Rewrite Cookie header so this request's later code can't re-read it
	cookies := r.Cookies()
	var rebuilt strings.Builder
	for _, ck := range cookies {
		if ck.Name == "flash" {
			continue
		}
		if rebuilt.Len() > 0 {
			rebuilt.WriteString("; ")
		}
		rebuilt.WriteString(ck.Name)
		rebuilt.WriteString("=")
		rebuilt.WriteString(ck.Value)
	}
	if rebuilt.Len() == 0 {
		r.Header.Del("Cookie")
	} else {
		r.Header.Set("Cookie", rebuilt.String())
	}
	return c.Value
}

func (h *Handler) ListAPITokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	tokens, err := model.ListAPITokensByUser(h.DB, user.ID)
	if err != nil {
		slog.Error("api_token: list", "user_id", user.ID, "error", err)
		h.render(w, r, "templates/settings/api_tokens.html", "API Tokens", apiTokenFormData{
			AvailableScopes: availableScopes(user),
			Errors:          map[string]string{"general": "Failed to load tokens"},
		})
		return
	}

	h.render(w, r, "templates/settings/api_tokens.html", "API Tokens", apiTokenFormData{
		Tokens:          tokens,
		AvailableScopes: availableScopes(user),
		Errors:          map[string]string{},
	})
}

func (h *Handler) NewAPIToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	data := apiTokenFormData{
		AvailableScopes: availableScopes(user),
		SelectedScopes:  map[string]bool{},
		Errors:          map[string]string{},
	}
	h.render(w, r, "templates/settings/api_tokens_form.html", "Create API Token", data)
}

func (h *Handler) CreateAPIToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	rawScopes := r.Form["scopes"]
	expRaw := strings.TrimSpace(r.FormValue("expires_at"))

	errs := map[string]string{}
	selectedMap := make(map[string]bool, len(rawScopes))
	for _, s := range rawScopes {
		selectedMap[s] = true
	}

	if name == "" {
		errs["name"] = "Name is required"
	}

	allowed := availableScopes(user)
	userCaps := make(map[string]bool, len(allowed))
	for _, c := range allowed {
		userCaps[c] = true
	}

	scopes := make([]string, 0, len(rawScopes))
	if len(rawScopes) == 0 {
		errs["scopes"] = "Select at least one scope"
	} else {
		for _, s := range rawScopes {
			if !userCaps[s] {
				errs["scopes"] = "Unknown or unauthorized scope: " + s
				break
			}
			scopes = append(scopes, s)
		}
	}

	var expiresAt *time.Time
	if expRaw != "" {
		t, err := time.Parse("2006-01-02", expRaw)
		if err != nil {
			errs["expires_at"] = "Invalid date format"
		} else {
			expiresAt = &t
		}
	}

	renderForm := func(data apiTokenFormData) {
		h.render(w, r, "templates/settings/api_tokens_form.html", "Create API Token", data)
	}

	if len(errs) > 0 {
		renderForm(apiTokenFormData{
			AvailableScopes: availableScopes(user),
			SelectedScopes:  selectedMap,
			Errors:          errs,
			Name:            name,
			ExpiresAt:       expRaw,
		})
		return
	}

	token, plaintext, err := model.CreateAPIToken(h.DB, user.ID, name, scopes, expiresAt)
	if err != nil {
		slog.Error("api_token: create", "user_id", user.ID, "error", err)
		fieldErrs := map[string]string{}
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			fieldErrs["name"] = "A token with this name already exists"
		} else {
			fieldErrs["general"] = "Failed to create token"
		}
		renderForm(apiTokenFormData{
			AvailableScopes: availableScopes(user),
			SelectedScopes:  selectedMap,
			Errors:          fieldErrs,
			Name:            name,
			ExpiresAt:       expRaw,
		})
		return
	}

	audit.Log(ctx, h.DB, audit.Event{
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

	h.setFlash(w, plaintext)
	http.Redirect(w, r, "/settings/api-tokens/created", http.StatusSeeOther)
}

func (h *Handler) CreatedAPIToken(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	plaintext := h.consumeFlash(w, r)
	if plaintext == "" {
		http.Redirect(w, r, "/settings/api-tokens", http.StatusSeeOther)
		return
	}

	w.Header().Set("Cache-Control", "no-store, private")
	h.render(w, r, "templates/settings/api_tokens_created.html", "Token Created", apiTokenFormData{
		Token: plaintext,
	})
}

func (h *Handler) RevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	id := r.PathValue("id")
	tokenID, err := strconv.Atoi(id)
	if err != nil || tokenID <= 0 {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	err = model.RevokeAPIToken(h.DB, user.ID, tokenID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.setFlash(w, "Token not found")
		} else {
			slog.Error("api_token: revoke", "user_id", user.ID, "token_id", tokenID, "error", err)
			h.setFlash(w, "Failed to revoke token")
		}
		http.Redirect(w, r, "/settings/api-tokens", http.StatusSeeOther)
		return
	}

	audit.Log(ctx, h.DB, audit.Event{
		Action:     "api_token.revoke",
		TargetType: "api_token",
		TargetID:   int64(tokenID),
	})

	h.setFlash(w, "Token revoked")
	http.Redirect(w, r, "/settings/api-tokens", http.StatusSeeOther)
}
