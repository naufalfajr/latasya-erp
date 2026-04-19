package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const sessionDuration = 7 * 24 * time.Hour

// SessionInfo holds the fields fetched together when resolving a session.
type SessionInfo struct {
	UserID    int
	CSRFToken string
}

func CreateSession(db *sql.DB, userID int) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	id := hex.EncodeToString(b)

	csrf, err := generateCSRFToken()
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(sessionDuration).UTC().Format(time.DateTime)
	_, err = db.Exec(
		"INSERT INTO sessions (id, user_id, expires_at, csrf_token) VALUES (?, ?, ?, ?)",
		id, userID, expiresAt, csrf,
	)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}

	return id, nil
}

// GetSession returns the user ID and CSRF token bound to the session in one
// query. Returns an error if the session is missing or expired.
func GetSession(db *sql.DB, sessionID string) (*SessionInfo, error) {
	s := &SessionInfo{}
	err := db.QueryRow(
		"SELECT user_id, csrf_token FROM sessions WHERE id = ? AND expires_at > datetime('now')",
		sessionID,
	).Scan(&s.UserID, &s.CSRFToken)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return s, nil
}

// GetSessionUserID is a convenience wrapper around GetSession for callers
// that only need the user ID.
func GetSessionUserID(db *sql.DB, sessionID string) (int, error) {
	s, err := GetSession(db, sessionID)
	if err != nil {
		return 0, err
	}
	return s.UserID, nil
}

func DeleteSession(db *sql.DB, sessionID string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}

// DeleteUserSessions removes all sessions for a user (used on login to prevent session fixation).
func DeleteUserSessions(db *sql.DB, userID int) error {
	_, err := db.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

func CleanExpiredSessions(db *sql.DB) {
	for {
		time.Sleep(1 * time.Hour)
		db.Exec("DELETE FROM sessions WHERE expires_at < datetime('now')")
	}
}
