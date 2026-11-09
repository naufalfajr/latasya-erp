// Package oauthas is the ERP's first-party OAuth 2.1 authorization server
// for the remote MCP endpoint: it issues single-use authorization codes
// from the browser consent page and exchanges them (PKCE S256) for ordinary
// lat_ tokens minted through the apitoken module. No refresh tokens — one
// flow, one purpose. Dynamic client registration (RFC 7591) mints an
// unpersisted public client_id; see internal/api/v1/oauthas.Handler.Register.
package oauthas

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/apitoken"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

var (
	ErrInvalidGrant = errors.New("invalid_grant")
	ErrInvalidScope = errors.New("invalid_scope")
)

type Module struct {
	db     *sql.DB
	access *access.Module
	tokens *apitoken.Module
}

func New(db *sql.DB) *Module {
	return &Module{db: db, access: access.New(db, nil), tokens: apitoken.New(db)}
}

// Grant is the consent decision the authorize endpoint stores.
type Grant struct {
	UserID        int
	Username      string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Scopes        []string
}

// Issue stores a consent decision and returns the plaintext authorization
// code exactly once. Only its SHA-256 hash is stored.
func (m *Module) Issue(ctx context.Context, g Grant) (string, error) {
	m.sweep(ctx)
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth code entropy: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(buf)
	encoded, err := json.Marshal(g.Scopes)
	if err != nil {
		return "", fmt.Errorf("encode oauth scopes: %w", err)
	}
	sum := sha256.Sum256([]byte(plaintext))
	if _, err := m.db.ExecContext(ctx,
		`INSERT INTO mcp_oauth_codes (code_hash, user_id, username, client_id, redirect_uri, code_challenge, scopes, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now', '+5 minutes'))`,
		hex.EncodeToString(sum[:]), g.UserID, g.Username, g.ClientID, g.RedirectURI, g.CodeChallenge, string(encoded)); err != nil {
		return "", fmt.Errorf("store oauth code: %w", err)
	}
	return plaintext, nil
}

// Exchange redeems an authorization code + PKCE verifier for a minted lat_
// token. The code is single-use, the redirect must match the authorize-time
// value exactly, and the consenter's capabilities are re-derived here — a
// role shrunk in the window fails with invalid_scope rather than smuggling.
func (m *Module) Exchange(ctx context.Context, plaintextCode, verifier, clientID, redirectURI string) (*apitoken.Created, []string, error) {
	m.sweep(ctx)
	sum := sha256.Sum256([]byte(plaintextCode))
	hash := hex.EncodeToString(sum[:])

	var (
		userID       int
		username     string
		storedClient string
		storedRedir  string
		challenge    string
		scopesJSON   string
	)
	err := m.db.QueryRowContext(ctx,
		`SELECT user_id, username, client_id, redirect_uri, code_challenge, scopes
		 FROM mcp_oauth_codes WHERE code_hash=? AND used_at IS NULL AND expires_at > datetime('now')`, hash).
		Scan(&userID, &username, &storedClient, &storedRedir, &challenge, &scopesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInvalidGrant
	}
	if err != nil {
		return nil, nil, fmt.Errorf("load oauth code: %w", err)
	}
	if storedClient != clientID || storedRedir != redirectURI {
		return nil, nil, ErrInvalidGrant
	}

	// PKCE S256: base64url(sha256(verifier)) must equal the stored challenge.
	verifierSum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(verifierSum[:]) != challenge {
		return nil, nil, ErrInvalidGrant
	}

	// Claim single-use atomically; a replayed code is invalid_grant.
	res, err := m.db.ExecContext(ctx,
		`UPDATE mcp_oauth_codes SET used_at = datetime('now') WHERE code_hash=? AND used_at IS NULL`, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("claim oauth code: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil, ErrInvalidGrant
	}

	var scopes []string
	_ = json.Unmarshal([]byte(scopesJSON), &scopes)

	user, err := m.access.LookupUserByIDForAuth(ctx, userID)
	if err != nil || !user.IsActive {
		return nil, nil, ErrInvalidGrant
	}
	if user.Role != model.RoleAdmin {
		if role, err := m.access.LookupRoleForAuth(ctx, user.Role); err == nil {
			user.Capabilities = role.Capabilities
		}
	}
	if !user.IsAdmin() {
		held := make(map[string]bool, len(user.Capabilities))
		for _, c := range user.Capabilities {
			held[c] = true
		}
		granted := make([]string, 0, len(scopes))
		for _, s := range scopes {
			if !held[s] {
				return nil, nil, ErrInvalidScope
			}
			granted = append(granted, s)
		}
		if len(granted) == 0 {
			return nil, nil, ErrInvalidScope
		}
		scopes = granted
	}

	// Random suffix: api_tokens enforces UNIQUE(user_id, name) and
	// revocation is soft-only, so re-consent on the same day must not
	// collide (same lesson as the device flow).
	var suffix [2]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return nil, nil, fmt.Errorf("oauth token name entropy: %w", err)
	}
	name := clientID
	if len(name) > 40 {
		name = strings.ToValidUTF8(name[:40], "")
	}
	created, err := m.tokens.Create(ctx,
		apitoken.Actor{
			UserID:       user.ID,
			Username:     user.Username,
			IsAdmin:      user.IsAdmin(),
			Capabilities: user.Capabilities,
		},
		apitoken.Draft{
			Name:      fmt.Sprintf("MCP Remote %s %s %x", name, time.Now().UTC().Format("2006-01-02"), suffix),
			Scopes:    scopes,
			ExpiresAt: tokenExpiry(30 * 24 * time.Hour),
		})
	if err != nil {
		return nil, nil, fmt.Errorf("mint oauth token: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{
		Action:        "mcp.token.issue",
		ActorID:       int64(user.ID),
		ActorUsername: user.Username,
		TargetType:    "api_token",
		TargetLabel:   created.Token.Name,
		Metadata:      map[string]any{"client_id": clientID, "scopes": scopes},
	})
	return created, scopes, nil
}

func tokenExpiry(d time.Duration) *time.Time {
	t := time.Now().UTC().Add(d)
	return &t
}

// sweep drops expired codes; runs inside Issue and Exchange, so no
// background goroutine is needed.
func (m *Module) sweep(ctx context.Context) {
	_, _ = m.db.ExecContext(ctx, `DELETE FROM mcp_oauth_codes WHERE expires_at < datetime('now')`)
}
