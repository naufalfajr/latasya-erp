package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
)

type apiTokenFormData struct {
	Tokens           []model.APIToken
	AvailableScopes []string
	Errors          map[string]string
	Token           string
}

func (h *Handler) ListAPITokens(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	tokens, err := model.ListAPITokensByUser(h.DB, user.ID)
	if err != nil {
		h.setFlash(w, "Failed to load tokens")
		h.render(w, r, "templates/settings/api_tokens.html", "API Tokens", apiTokenFormData{
			Errors: map[string]string{"general": "Failed to load tokens"},
		})
		return
	}

	h.render(w, r, "templates/settings/api_tokens.html", "API Tokens", apiTokenFormData{
		Tokens:           tokens,
		AvailableScopes: user.Capabilities,
	})
}

func (h *Handler) NewAPIToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	isHTMX := r.Header.Get("HX-Request") == "true"
	data := apiTokenFormData{
		AvailableScopes: user.Capabilities,
		Errors:          make(map[string]string),
	}

	if isHTMX {
		h.render(w, r, "templates/settings/api_tokens_new_partial.html", "Create API Token", data)
	} else {
		h.render(w, r, "templates/settings/api_tokens_new.html", "Create API Token", data)
	}
}

func (h *Handler) CreateAPIToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	name := r.FormValue("name")
	Scopes := r.Form["scopes"]
	var expiresAt *time.Time
	isHTMX := r.Header.Get("HX-Request") == "true"

	if expStr := r.FormValue("expires_at"); expStr != "" {
		t, err := time.Parse("2006-01-02", expStr)
		if err != nil {
			data := apiTokenFormData{
				AvailableScopes: user.Capabilities,
				Errors:          map[string]string{"expires_at": "Invalid date format"},
			}
			if isHTMX {
				h.render(w, r, "templates/settings/api_tokens_new_partial.html", "Create API Token", data)
			} else {
				h.render(w, r, "templates/settings/api_tokens_new.html", "Create API Token", data)
			}
			return
		}
		expiresAt = &t
	}

	userCaps := make(map[string]bool)
	for _, cap := range user.Capabilities {
		userCaps[cap] = true
	}
	scopes := make([]string, 0, len(Scopes))
	for _, s := range Scopes {
		if !userCaps[s] {
			data := apiTokenFormData{
				AvailableScopes: user.Capabilities,
				Errors:          map[string]string{"scopes": "You don't have permission: " + s},
			}
			if isHTMX {
				h.render(w, r, "templates/settings/api_tokens_new_partial.html", "Create API Token", data)
			} else {
				h.render(w, r, "templates/settings/api_tokens_new.html", "Create API Token", data)
			}
			return
		}
		scopes = append(scopes, s)
	}

	token, plaintext, err := model.CreateAPIToken(h.DB, user.ID, name, scopes, expiresAt)
	if err != nil {
		data := apiTokenFormData{
			AvailableScopes: user.Capabilities,
			Errors:          map[string]string{"general": err.Error()},
		}
		if isHTMX {
			h.render(w, r, "templates/settings/api_tokens_new_partial.html", "Create API Token", data)
		} else {
			h.render(w, r, "templates/settings/api_tokens_new.html", "Create API Token", data)
		}
		return
	}

	audit.Log(ctx, h.DB, audit.Event{
		Action: "api_token.create",
		TargetType: "api_token",
		TargetID: int64(token.ID),
		TargetLabel: token.Name,
		Metadata: map[string]any{
			"scopes": token.Scopes,
			"expires_at": token.ExpiresAt,
		},
	})

	data := apiTokenFormData{Token: plaintext}

	if isHTMX {
		h.render(w, r, "templates/settings/api_tokens_created_partial.html", "Token Created", data)
	} else {
		h.render(w, r, "templates/settings/api_tokens_created.html", "Token Created", data)
	}
}

func (h *Handler) RevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	path := r.URL.Path
	var tokenID int
	_, err := fmt.Sscanf(path, "/settings/api-tokens/%d/revoke", &tokenID)
	if err != nil {
		http.Error(w, "Invalid token ID", http.StatusBadRequest)
		return
	}

	err = model.RevokeAPIToken(h.DB, user.ID, tokenID)
	if err != nil {
		h.setFlash(w, "Failed to revoke token")
	}

	audit.Log(ctx, h.DB, audit.Event{
		Action: "api_token.revoke",
		TargetType: "api_token",
		TargetID: int64(tokenID),
	})

	http.Redirect(w, r, "/settings/api-tokens", http.StatusFound)
}