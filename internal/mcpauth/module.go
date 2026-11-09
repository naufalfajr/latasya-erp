// Package mcpauth owns the device-code login handoff between the browser
// (consent page) and the local MCP client binary. The client polls with a
// hashed device code; the approving user's decision is stored, never the
// token — the token is minted at consume time via the apitoken module, with
// the approver's capabilities re-derived from the database at that moment.
package mcpauth

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/apitoken"
	"github.com/naufal/latasya-erp/internal/model"
)

var (
	ErrPending     = errors.New("handoff pending")
	ErrDenied      = errors.New("handoff denied")
	ErrExpired     = errors.New("handoff expired")
	ErrGone        = errors.New("handoff already consumed or unknown")
	ErrInvalidCode = errors.New("unknown or expired login code")
	ErrScopeChange = errors.New("capabilities changed since approval; restart login")
)

type Module struct {
	db     *sql.DB
	access *access.Module
	tokens *apitoken.Module
}

func New(db *sql.DB) *Module {
	return &Module{db: db, access: access.New(db, nil), tokens: apitoken.New(db)}
}

// Handoff is the state of one login attempt as seen by the polling client.
type Handoff struct {
	UserCode  string
	Client    string
	Status    string
	UserID    int
	Username  string
	Scopes    []string
	ExpiresOn *time.Time
}

// sweep drops rows past their TTL. It runs inside Touch and Consume, so no
// background goroutine is needed; the index on expires_at keeps it cheap.
func (m *Module) sweep(ctx context.Context) {
	_, _ = m.db.ExecContext(ctx, `DELETE FROM mcp_handoffs WHERE expires_at < datetime('now')`)
}

// Touch upserts the pending row for a device (first poll creates it) and
// returns the handoff's current status. A device code that arrives with a
// different user code than the stored row means the client restarted login:
// the old row is reported gone.
func (m *Module) Touch(ctx context.Context, deviceHash, userCode, client string) (Handoff, error) {
	m.sweep(ctx)
	if _, err := m.db.ExecContext(ctx,
		`INSERT INTO mcp_handoffs (device_code_hash, user_code, client, status, expires_at)
		 VALUES (?, ?, ?, 'pending', datetime('now', '+10 minutes'))
		 ON CONFLICT(device_code_hash) DO NOTHING`,
		deviceHash, userCode, client); err != nil {
		// A user code already in flight (UNIQUE(user_code)) is not covered by
		// the ON CONFLICT clause above; the client restarted login and must
		// regenerate both codes.
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Handoff{}, ErrGone
		}
		return Handoff{}, fmt.Errorf("mcp handoff upsert: %w", err)
	}
	h, err := m.byDevice(ctx, deviceHash)
	if err != nil {
		return Handoff{}, err
	}
	if h.UserCode != userCode {
		return Handoff{}, ErrGone
	}
	return h, nil
}

// Approve records the consenting user's decision. No token exists yet.
func (m *Module) Approve(ctx context.Context, userCode string, userID int, username string, scopes []string, tokenExpiresAt *time.Time) error {
	encoded, err := json.Marshal(scopes)
	if err != nil {
		return fmt.Errorf("encode mcp handoff scopes: %w", err)
	}
	var expiry any
	if tokenExpiresAt != nil {
		expiry = tokenExpiresAt.UTC().Format(time.RFC3339)
	}
	res, err := m.db.ExecContext(ctx,
		`UPDATE mcp_handoffs
		 SET status='approved', approved_user_id=?, approved_username=?, scopes=?, token_expires_at=?
		 WHERE user_code=? AND status='pending' AND expires_at > datetime('now')`,
		userID, username, string(encoded), expiry, userCode)
	if err != nil {
		return fmt.Errorf("mcp handoff approve: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrInvalidCode
	}
	return nil
}

// Deny marks a pending handoff denied; the client's next poll gets 403.
func (m *Module) Deny(ctx context.Context, userCode string) error {
	res, err := m.db.ExecContext(ctx,
		`UPDATE mcp_handoffs SET status='denied' WHERE user_code=? AND status='pending' AND expires_at > datetime('now')`,
		userCode)
	if err != nil {
		return fmt.Errorf("mcp handoff deny: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrInvalidCode
	}
	return nil
}

// Lookup returns the pending handoff for a consent page by user code.
func (m *Module) Lookup(ctx context.Context, userCode string) (Handoff, error) {
	m.sweep(ctx)
	return m.byUser(ctx, userCode)
}

// Consume claims an approved handoff and mints the token. The row is deleted
// before minting: if minting fails the handoff is gone and the user simply
// re-runs login (no half-issued state). The approver's capabilities are
// re-derived from the database here, so a role change in the approve/poll
// window can never smuggle scopes through; apitoken.Create re-validates
// scopes ⊆ current capabilities on top.
func (m *Module) Consume(ctx context.Context, deviceHash string) (*apitoken.Created, Handoff, error) {
	m.sweep(ctx)
	h, err := m.byDevice(ctx, deviceHash)
	if err != nil {
		return nil, Handoff{}, err
	}
	switch h.Status {
	case "pending":
		return nil, h, ErrPending
	case "denied":
		return nil, h, ErrDenied
	}

	res, err := m.db.ExecContext(ctx,
		`DELETE FROM mcp_handoffs WHERE device_code_hash=? AND status='approved'`, deviceHash)
	if err != nil {
		return nil, h, fmt.Errorf("mcp handoff consume: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return nil, h, ErrGone
	}

	user, err := m.access.LookupUserByIDForAuth(ctx, h.UserID)
	if err != nil || !user.IsActive {
		return nil, h, ErrDenied
	}
	if user.Role != model.RoleAdmin {
		if role, err := m.access.LookupRoleForAuth(ctx, user.Role); err == nil {
			user.Capabilities = role.Capabilities
		}
	}

	name := h.Client
	if len(name) > 40 {
		name = strings.ToValidUTF8(name[:40], "")
	}
	// api_tokens enforces UNIQUE(user_id, name) and revocation is soft-only,
	// so a same-day re-login (logout→login, renew, second machine with the
	// same client string) would collide forever with a purely deterministic
	// name. A short random suffix makes every mint unique.
	var suffix [2]byte
	if _, err := crand.Read(suffix[:]); err != nil {
		return nil, h, fmt.Errorf("mcp token name entropy: %w", err)
	}
	tokenName := fmt.Sprintf("MCP %s %s %x", name, time.Now().UTC().Format("2006-01-02"), suffix)
	created, err := m.tokens.Create(ctx,
		apitoken.Actor{
			UserID:       user.ID,
			Username:     user.Username,
			IsAdmin:      user.IsAdmin(),
			Capabilities: user.Capabilities,
		},
		apitoken.Draft{
			Name:      tokenName,
			Scopes:    h.Scopes,
			ExpiresAt: h.ExpiresOn,
		})
	if err != nil {
		var validation *apitoken.ValidationError
		if errors.As(err, &validation) {
			return nil, h, ErrScopeChange
		}
		return nil, h, err
	}
	return created, h, nil
}

func (m *Module) byDevice(ctx context.Context, deviceHash string) (Handoff, error) {
	return m.scan(m.db.QueryRowContext(ctx,
		`SELECT user_code, client, status, approved_user_id, approved_username, scopes, token_expires_at, expires_at
		 FROM mcp_handoffs WHERE device_code_hash=?`, deviceHash))
}

func (m *Module) byUser(ctx context.Context, userCode string) (Handoff, error) {
	return m.scan(m.db.QueryRowContext(ctx,
		`SELECT user_code, client, status, approved_user_id, approved_username, scopes, token_expires_at, expires_at
		 FROM mcp_handoffs WHERE user_code=?`, userCode))
}

func (m *Module) scan(row *sql.Row) (Handoff, error) {
	var h Handoff
	var scopes string
	var tokenExpiresOn, rowExpiresAt sql.NullString
	var userID sql.NullInt64
	err := row.Scan(&h.UserCode, &h.Client, &h.Status, &userID, &h.Username, &scopes, &tokenExpiresOn, &rowExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Handoff{}, ErrGone
	}
	if err != nil {
		return Handoff{}, fmt.Errorf("mcp handoff load: %w", err)
	}
	h.UserID = int(userID.Int64)
	_ = json.Unmarshal([]byte(scopes), &h.Scopes)
	if tokenExpiresOn.Valid && tokenExpiresOn.String != "" {
		if t, err := time.Parse(time.RFC3339, tokenExpiresOn.String); err == nil {
			h.ExpiresOn = &t
		}
	}
	// SQLite stores both as UTC 'YYYY-MM-DD HH:MM:SS', so string comparison
	// with an equally formatted now is an ordering-safe expiry check.
	if rowExpiresAt.Valid && rowExpiresAt.String <= time.Now().UTC().Format("2006-01-02 15:04:05") {
		return h, ErrExpired
	}
	return h, nil
}
