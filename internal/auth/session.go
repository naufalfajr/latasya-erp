package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const sessionDuration = 7 * 24 * time.Hour

func CreateSession(db *sql.DB, userID int) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	id := hex.EncodeToString(b)

	expiresAt := time.Now().Add(sessionDuration).UTC().Format(time.DateTime)
	_, err := db.Exec(
		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
		id, userID, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}

	return id, nil
}

func GetSessionUserID(db *sql.DB, sessionID string) (int, error) {
	var userID int
	err := db.QueryRow(
		"SELECT user_id FROM sessions WHERE id = ? AND expires_at > datetime('now')",
		sessionID,
	).Scan(&userID)
	if err != nil {
		return 0, fmt.Errorf("get session: %w", err)
	}
	return userID, nil
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
