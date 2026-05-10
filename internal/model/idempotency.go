package model

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"
)

// IdempotencyRecord stores a cached response for replay.
type IdempotencyRecord struct {
	Key            string
	UserID         int
	RequestHash    string
	ResponseStatus int
	ResponseBody   []byte
	ExpiresAt      time.Time
}

// LookupIdempotency looks up an existing idempotency record for (key, userID).
// Returns (nil, nil) if not found or expired. Records past their expires_at
// are treated as missing so clients can reuse a key after the TTL window.
func LookupIdempotency(db *sql.DB, key string, userID int) (*IdempotencyRecord, error) {
	var rec IdempotencyRecord
	var expiresAt string
	err := db.QueryRow(`
        SELECT key, user_id, request_hash, response_status, response_body, expires_at
        FROM idempotency_keys
        WHERE key = ? AND user_id = ? AND expires_at > datetime('now')
    `, key, userID).Scan(
		&rec.Key, &rec.UserID, &rec.RequestHash, &rec.ResponseStatus, &rec.ResponseBody, &expiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup idempotency: %w", err)
	}
	if ts, err := time.Parse(time.RFC3339, expiresAt); err == nil {
		rec.ExpiresAt = ts
	}
	return &rec, nil
}

// StoreIdempotency stores a response for future replay. Uses INSERT OR IGNORE
// so a concurrent winner's row is preserved (the loser silently no-ops).
// TTL is fixed at 24h from now and is NOT extended on replay.
func StoreIdempotency(db *sql.DB, key string, userID int, requestHash string, status int, body []byte) error {
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	_, err := db.Exec(`
        INSERT OR IGNORE INTO idempotency_keys (key, user_id, request_hash, response_status, response_body, expires_at)
        VALUES (?, ?, ?, ?, ?, ?)
    `, key, userID, requestHash, status, body, expiresAt)
	if err != nil {
		return fmt.Errorf("store idempotency: %w", err)
	}
	return nil
}

// CleanExpiredIdempotencyKeys deletes expired idempotency records. Logs but
// does not return errors — it's invoked from a background ticker goroutine.
func CleanExpiredIdempotencyKeys(db *sql.DB) {
	if _, err := db.Exec(`DELETE FROM idempotency_keys WHERE expires_at < datetime('now')`); err != nil {
		slog.Error("clean expired idempotency keys", "error", err)
	}
}

// HashRequest computes a sha256 hash of the request for idempotency comparison.
// Includes user_id so the same key cannot collide across users (defense in depth
// since the table also keys on user_id).
func HashRequest(userID int, method, path string, body []byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d:%s:%s:", userID, method, path)
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
