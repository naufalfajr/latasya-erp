package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	// sessionIdleTimeout is the sliding idle window. Each authenticated
	// request refreshes expires_at to now + this value, so an idle user is
	// logged out after this much inactivity.
	sessionIdleTimeout = 10 * time.Hour

	// sessionAbsoluteMaxAge is the hard cap on a session's lifetime from
	// creation. TouchSession never pushes expires_at past this point, so
	// even a continuously-active session must re-login after this long.
	sessionAbsoluteMaxAge = 72 * time.Hour

	// sessionRefreshThreshold throttles writes: RequireAuth only calls
	// TouchSession when the remaining idle time drops below this. At worst
	// the stored expiry lags real activity by this much, which is fine for
	// a 10h idle window.
	sessionRefreshThreshold = 5 * time.Hour
)

// SessionInfo holds the fields fetched together when resolving a session.
type SessionInfo struct {
	UserID    int
	CSRFToken string
	ExpiresAt time.Time
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

	now := time.Now().UTC()
	idleExpiry := now.Add(sessionIdleTimeout).Format(time.DateTime)
	absoluteExpiry := now.Add(sessionAbsoluteMaxAge).Format(time.DateTime)
	_, err = db.Exec(
		"INSERT INTO sessions (id, user_id, expires_at, absolute_expires_at, csrf_token) VALUES (?, ?, ?, ?, ?)",
		id, userID, idleExpiry, absoluteExpiry, csrf,
	)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}

	return id, nil
}

// GetSession returns the user ID, CSRF token, and idle deadline bound to the
// session in one query. Returns an error if the session is missing, past its
// idle window, or past its absolute cap.
func GetSession(db *sql.DB, sessionID string) (*SessionInfo, error) {
	s := &SessionInfo{}
	var expiresAtStr string
	err := db.QueryRow(
		`SELECT user_id, csrf_token, expires_at
		 FROM sessions
		 WHERE id = ?
		   AND expires_at > datetime('now')
		   AND absolute_expires_at > datetime('now')`,
		sessionID,
	).Scan(&s.UserID, &s.CSRFToken, &expiresAtStr)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	t, err := time.Parse(time.DateTime, expiresAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse expires_at: %w", err)
	}
	s.ExpiresAt = t
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

// TouchSession slides the idle deadline forward by sessionIdleTimeout, but
// never past absolute_expires_at. Safe to call on any valid session id; a
// no-op for unknown ids.
func TouchSession(db *sql.DB, sessionID string) error {
	nextExpiry := time.Now().Add(sessionIdleTimeout).UTC().Format(time.DateTime)
	_, err := db.Exec(
		`UPDATE sessions
		 SET expires_at = MIN(?, absolute_expires_at)
		 WHERE id = ?`,
		nextExpiry, sessionID,
	)
	return err
}

// ShouldRefresh reports whether a session's idle window is close enough to
// expiring that it's worth writing a fresh expires_at.
func ShouldRefresh(s *SessionInfo) bool {
	return time.Until(s.ExpiresAt) < sessionRefreshThreshold
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
		db.Exec(
			`DELETE FROM sessions
			 WHERE expires_at < datetime('now')
			    OR absolute_expires_at < datetime('now')`,
		)
	}
}
