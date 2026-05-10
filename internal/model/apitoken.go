package model

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// APIToken represents a scoped Bearer token for bot/MCP/Telegram integrations.
// The plaintext token is NEVER stored — only the sha256 hash.
type APIToken struct {
	ID          int
	UserID      int
	Name        string
	TokenPrefix string   // first 8 chars of plaintext (safe to display)
	Scopes      []string // subset of user's capabilities at creation time
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

// GenerateAPIToken generates a new API token.
// Returns: plaintext (show ONCE), prefix (safe to display), hash (store in DB).
func GenerateAPIToken() (plaintext, prefix, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generate token: %w", err)
	}

	chars := make([]byte, 32)
	for i, byt := range b {
		chars[i] = base62Alphabet[int(byt)%62]
	}

	plaintext = "lat_" + string(chars)
	prefix = plaintext[:8]

	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])
	return plaintext, prefix, hash, nil
}

// CreateAPIToken creates a new API token for the given user.
// Returns the model (without hash) and the plaintext (caller must show ONCE).
func CreateAPIToken(db *sql.DB, userID int, name string, scopes []string, expiresAt *time.Time) (*APIToken, string, error) {
	plaintext, prefix, hash, err := GenerateAPIToken()
	if err != nil {
		return nil, "", err
	}

	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return nil, "", fmt.Errorf("marshal scopes: %w", err)
	}

	var expiresAtStr *string
	if expiresAt != nil {
		s := expiresAt.UTC().Format(time.RFC3339)
		expiresAtStr = &s
	}

	result, err := db.Exec(`
		INSERT INTO api_tokens (user_id, name, token_prefix, token_hash, scopes, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userID, name, prefix, hash, string(scopesJSON), expiresAtStr)
	if err != nil {
		return nil, "", fmt.Errorf("insert api_token: %w", err)
	}

	id, _ := result.LastInsertId()
	token := &APIToken{
		ID:          int(id),
		UserID:      userID,
		Name:        name,
		TokenPrefix: prefix,
		Scopes:      scopes,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now().UTC(),
	}
	return token, plaintext, nil
}

// GetAPITokenByHash looks up a token by its sha256 hash.
// Returns sql.ErrNoRows if not found, revoked, or expired.
// Updates last_used_at asynchronously (fire-and-forget).
func GetAPITokenByHash(db *sql.DB, hash string) (*APIToken, error) {
	var t APIToken
	var scopesJSON string
	var expiresAt, lastUsedAt, revokedAt, createdAt sql.NullString

	err := db.QueryRow(`
		SELECT id, user_id, name, token_prefix, scopes, expires_at, last_used_at, revoked_at, created_at
		FROM api_tokens
		WHERE token_hash = ?
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR datetime(expires_at) > datetime('now'))
	`, hash).Scan(
		&t.ID, &t.UserID, &t.Name, &t.TokenPrefix, &scopesJSON,
		&expiresAt, &lastUsedAt, &revokedAt, &createdAt,
	)
	if err != nil {
		return nil, err // includes sql.ErrNoRows
	}

	if err := json.Unmarshal([]byte(scopesJSON), &t.Scopes); err != nil {
		return nil, fmt.Errorf("unmarshal scopes: %w", err)
	}

	if expiresAt.Valid {
		if ts, err := time.Parse(time.RFC3339, expiresAt.String); err == nil {
			t.ExpiresAt = &ts
		}
	}
	if createdAt.Valid {
		if ts, err := time.Parse(time.RFC3339, createdAt.String); err == nil {
			t.CreatedAt = ts
		}
	}

	// Update last_used_at asynchronously
	go func() {
		if _, err := db.Exec(`UPDATE api_tokens SET last_used_at = datetime('now') WHERE id = ?`, t.ID); err != nil {
			slog.Error("api_token: update last_used_at", "id", t.ID, "error", err)
		}
	}()

	return &t, nil
}

// ListAPITokensByUser returns all tokens for a user (never includes hash).
func ListAPITokensByUser(db *sql.DB, userID int) ([]APIToken, error) {
	rows, err := db.Query(`
		SELECT id, user_id, name, token_prefix, scopes, expires_at, last_used_at, revoked_at, created_at
		FROM api_tokens
		WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		var scopesJSON string
		var expiresAt, lastUsedAt, revokedAt, createdAt sql.NullString

		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenPrefix, &scopesJSON,
			&expiresAt, &lastUsedAt, &revokedAt, &createdAt); err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(scopesJSON), &t.Scopes); err != nil {
			return nil, fmt.Errorf("unmarshal scopes: %w", err)
		}

		if expiresAt.Valid {
			if ts, err := time.Parse(time.RFC3339, expiresAt.String); err == nil {
				t.ExpiresAt = &ts
			}
		}
		if lastUsedAt.Valid {
			if ts, err := time.Parse(time.RFC3339, lastUsedAt.String); err == nil {
				t.LastUsedAt = &ts
			}
		}
		if revokedAt.Valid {
			if ts, err := time.Parse(time.RFC3339, revokedAt.String); err == nil {
				t.RevokedAt = &ts
			}
		}
		if createdAt.Valid {
			if ts, err := time.Parse(time.RFC3339, createdAt.String); err == nil {
				t.CreatedAt = ts
			}
		}

		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// RevokeAPIToken revokes a token. Only the owning user can revoke their own tokens.
func RevokeAPIToken(db *sql.DB, userID, tokenID int) error {
	result, err := db.Exec(`
		UPDATE api_tokens SET revoked_at = datetime('now')
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL
	`, tokenID, userID)
	if err != nil {
		return fmt.Errorf("revoke api_token: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows // not found or already revoked or wrong user
	}
	return nil
}
