package idempotency

import (
	"context"
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

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

// LookupIdempotency looks up an existing idempotency record for (key, userID).
// Returns (nil, nil) if not found or expired. Records past their expires_at
// are treated as missing so clients can reuse a key after the TTL window.
func (s *Store) Lookup(ctx context.Context, key string, userID int) (*IdempotencyRecord, error) {
	var rec IdempotencyRecord
	var expiresAt string
	err := s.db.QueryRowContext(ctx, `
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
	if ts, err := time.Parse(time.DateTime, expiresAt); err == nil {
		rec.ExpiresAt = ts
	}
	return &rec, nil
}

// Save stores a response for future replay. A live concurrent winner is
// preserved, while an expired row is replaced so its key can be reused.
func (s *Store) Save(ctx context.Context, key string, userID int, requestHash string, status int, body []byte) error {
	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format(time.DateTime)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO idempotency_keys (key, user_id, request_hash, response_status, response_body, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(key, user_id) DO UPDATE SET
			request_hash=excluded.request_hash,
			response_status=excluded.response_status,
			response_body=excluded.response_body,
			expires_at=excluded.expires_at,
			created_at=datetime('now')
		WHERE idempotency_keys.expires_at <= datetime('now')
	`, key, userID, requestHash, status, body, expiresAt)
	if err != nil {
		return fmt.Errorf("store idempotency: %w", err)
	}
	return nil
}

// CleanExpiredIdempotencyKeys deletes expired idempotency records. Logs but
// does not return errors — it's invoked from a background ticker goroutine.
func (s *Store) CleanExpired(ctx context.Context) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at <= datetime('now')`); err != nil {
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
