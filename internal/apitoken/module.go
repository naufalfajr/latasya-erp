package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/model"
)

var (
	ErrForbidden = errors.New("api token access denied")
	ErrNotFound  = errors.New("api token not found")
)

type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "validation failed" }

type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }

type Actor struct {
	UserID         int
	Username       string
	IsAdmin        bool
	Capabilities   []string
	CanManageUsers bool
}

type Draft struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

type Created struct {
	Token     *model.APIToken
	Plaintext string
}

type Module struct {
	db     *sql.DB
	access *access.Module
}

func New(db *sql.DB) *Module { return &Module{db: db, access: access.New(db, nil)} }

func (m *Module) Create(ctx context.Context, actor Actor, draft Draft) (*Created, error) {
	if actor.UserID <= 0 {
		return nil, ErrForbidden
	}
	draft.Name = strings.TrimSpace(draft.Name)
	fields := map[string]string{}
	if draft.Name == "" {
		fields["name"] = "required"
	}
	if draft.ExpiresAt != nil && !draft.ExpiresAt.After(time.Now().UTC()) {
		fields["expires_at"] = "must be in the future"
	}
	if bad, ok := allowedScopes(draft.Scopes, actor); !ok {
		fields["scopes"] = "scope " + fmt.Sprintf("%q", bad) + " is not in your capabilities"
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	plaintext, prefix, hash, err := generate()
	if err != nil {
		return nil, fmt.Errorf("generate api token: %w", err)
	}
	scopes := draft.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	encoded, err := json.Marshal(scopes)
	if err != nil {
		return nil, fmt.Errorf("encode api token scopes: %w", err)
	}
	var expiry any
	if draft.ExpiresAt != nil {
		expiry = draft.ExpiresAt.UTC().Format(time.RFC3339)
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin api token create: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO api_tokens (user_id,name,token_prefix,token_hash,scopes,expires_at) VALUES (?,?,?,?,?,?)`, actor.UserID, draft.Name, prefix, hash, string(encoded), expiry)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, &ConflictError{Message: "a token with this name already exists"}
		}
		return nil, fmt.Errorf("create api token: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("api token id: %w", err)
	}
	token, err := getOwnedWith(ctx, tx, actor.UserID, int(id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit api token create: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "api_token.create", ActorID: int64(actor.UserID), ActorUsername: actor.Username, TargetType: "api_token", TargetID: id, TargetLabel: token.Name, Metadata: map[string]any{"name": token.Name, "scopes": token.Scopes, "expires_at": token.ExpiresAt}})
	return &Created{Token: token, Plaintext: plaintext}, nil
}

func (m *Module) List(ctx context.Context, actor Actor) ([]model.APIToken, error) {
	if actor.UserID <= 0 {
		return nil, ErrForbidden
	}
	rows, err := m.db.QueryContext(ctx, `SELECT id,user_id,name,token_prefix,scopes,expires_at,last_used_at,revoked_at,created_at FROM api_tokens WHERE user_id=? ORDER BY created_at DESC`, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer rows.Close()
	result := []model.APIToken{}
	for rows.Next() {
		token, err := scanToken(rows, false)
		if err != nil {
			return nil, err
		}
		result = append(result, *token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api tokens: %w", err)
	}
	return result, nil
}

// ListAll returns every user's tokens, active ones first.
// ponytail: unpaginated; fine for a small staff, page it if tokens reach the hundreds.
func (m *Module) ListAll(ctx context.Context, actor Actor) ([]model.APIToken, error) {
	if err := requireUserManager(actor); err != nil {
		return nil, err
	}
	rows, err := m.db.QueryContext(ctx, `SELECT `+ownedColumns+` FROM api_tokens t JOIN users u ON u.id=t.user_id ORDER BY t.revoked_at IS NOT NULL, t.created_at DESC, t.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list all api tokens: %w", err)
	}
	defer rows.Close()
	result := []model.APIToken{}
	for rows.Next() {
		token, err := scanToken(rows, true)
		if err != nil {
			return nil, err
		}
		result = append(result, *token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate all api tokens: %w", err)
	}
	return result, nil
}

func (m *Module) LookupByHash(ctx context.Context, hash string) (*model.APIToken, error) {
	token, err := scanToken(m.db.QueryRowContext(ctx, `SELECT id,user_id,name,token_prefix,scopes,expires_at,last_used_at,revoked_at,created_at FROM api_tokens WHERE token_hash=? AND revoked_at IS NULL AND (expires_at IS NULL OR datetime(expires_at)>datetime('now'))`, hash), false)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup api token: %w", err)
	}
	// Coarse best-effort telemetry avoids a write on every authenticated request.
	_, _ = m.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at=datetime('now') WHERE id=? AND (last_used_at IS NULL OR datetime(last_used_at)<datetime('now','-1 minute'))`, token.ID)
	return token, nil
}

func (m *Module) Authenticate(ctx context.Context, plaintext string) (*model.APIToken, error) {
	digest := sha256.Sum256([]byte(plaintext))
	return m.LookupByHash(ctx, hex.EncodeToString(digest[:]))
}

func (m *Module) Revoke(ctx context.Context, actor Actor, tokenID int) (*model.APIToken, error) {
	if actor.UserID <= 0 {
		return nil, ErrForbidden
	}
	return m.revoke(ctx, actor, actor.UserID, "", tokenID)
}

// RevokeAny revokes another user's token, e.g. after a password reset, without
// deactivating them; like user management, only for owners the actor may manage.
func (m *Module) RevokeAny(ctx context.Context, actor Actor, tokenID int) (*model.APIToken, error) {
	if err := requireUserManager(actor); err != nil {
		return nil, err
	}
	var ownerID int
	var ownerRole string
	err := m.db.QueryRowContext(ctx, `SELECT t.user_id, u.role FROM api_tokens t JOIN users u ON u.id=t.user_id WHERE t.id=?`, tokenID).Scan(&ownerID, &ownerRole)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get api token owner: %w", err)
	}
	manager := access.Actor{UserID: actor.UserID, CanManageUsers: actor.CanManageUsers, IsAdmin: actor.IsAdmin, Capabilities: actor.Capabilities}
	if err := m.access.CheckManageable(ctx, manager, ownerRole); err != nil {
		if errors.Is(err, access.ErrForbidden) {
			return nil, ErrForbidden
		}
		return nil, err
	}
	return m.revoke(ctx, actor, ownerID, ownerRole, tokenID)
}

// revoke refuses when approvedRole is set and the owner's role changed since it
// was approved, so a promotion can't race RevokeAny's check.
func (m *Module) revoke(ctx context.Context, actor Actor, ownerID int, approvedRole string, tokenID int) (*model.APIToken, error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin api token revoke: %w", err)
	}
	defer tx.Rollback()
	token, err := getOwnedWith(ctx, tx, ownerID, tokenID)
	if err != nil {
		return nil, err
	}
	if approvedRole != "" && token.OwnerRole != approvedRole {
		return nil, ErrForbidden
	}
	result, err := tx.ExecContext(ctx, `UPDATE api_tokens SET revoked_at=datetime('now') WHERE id=? AND user_id=? AND revoked_at IS NULL`, tokenID, ownerID)
	if err != nil {
		return nil, fmt.Errorf("revoke api token: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit api token revoke: %w", err)
	}
	audit.Log(ctx, m.db, audit.Event{Action: "api_token.revoke", ActorID: int64(actor.UserID), ActorUsername: actor.Username, TargetType: "api_token", TargetID: int64(token.ID), TargetLabel: token.Name, Metadata: map[string]any{"token_id": token.ID, "name": token.Name, "owner_user_id": ownerID, "owner_username": token.OwnerUsername}})
	return token, nil
}

type scanner interface{ Scan(...any) error }

func requireUserManager(actor Actor) error {
	if actor.UserID <= 0 || !actor.CanManageUsers {
		return ErrForbidden
	}
	return nil
}

// ownedColumns are scanToken's columns plus the owner, for api_tokens t JOIN users u.
const ownedColumns = "t.id,t.user_id,t.name,t.token_prefix,t.scopes,t.expires_at,t.last_used_at,t.revoked_at,t.created_at,u.username,u.full_name,u.role"

func scanToken(row scanner, withOwner bool) (*model.APIToken, error) {
	var token model.APIToken
	var scopes string
	var expires, used, revoked, created sql.NullString
	dest := []any{&token.ID, &token.UserID, &token.Name, &token.TokenPrefix, &scopes, &expires, &used, &revoked, &created}
	if withOwner {
		dest = append(dest, &token.OwnerUsername, &token.OwnerName, &token.OwnerRole)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(scopes), &token.Scopes); err != nil {
		return nil, fmt.Errorf("decode api token scopes: %w", err)
	}
	token.ExpiresAt = parseNullable(expires)
	token.LastUsedAt = parseNullable(used)
	token.RevokedAt = parseNullable(revoked)
	if parsed := parseNullable(created); parsed != nil {
		token.CreatedAt = *parsed
	}
	return &token, nil
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getOwnedWith(ctx context.Context, q rowQueryer, userID, tokenID int) (*model.APIToken, error) {
	token, err := scanToken(q.QueryRowContext(ctx, `SELECT `+ownedColumns+` FROM api_tokens t JOIN users u ON u.id=t.user_id WHERE t.id=? AND t.user_id=?`, tokenID, userID), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get api token: %w", err)
	}
	return token, nil
}

func allowedScopes(scopes []string, actor Actor) (string, bool) {
	if actor.IsAdmin {
		allowed := map[string]bool{}
		for _, capability := range model.AllCapabilities {
			allowed[capability] = true
		}
		for _, scope := range scopes {
			if !allowed[scope] {
				return scope, false
			}
		}
		return "", true
	}
	allowed := map[string]bool{}
	for _, capability := range actor.Capabilities {
		allowed[capability] = true
	}
	for _, scope := range scopes {
		if !allowed[scope] {
			return scope, false
		}
	}
	return "", true
}

func parseNullable(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value.String); err == nil {
			return &parsed
		}
	}
	return nil
}

func generate() (plaintext, prefix, hash string, err error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		return "", "", "", err
	}
	chars := make([]byte, len(random))
	for i, value := range random {
		chars[i] = alphabet[int(value)%len(alphabet)]
	}
	plaintext = "lat_" + string(chars)
	prefix = plaintext[:8]
	digest := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(digest[:])
	return plaintext, prefix, hash, nil
}
