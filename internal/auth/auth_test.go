package auth_test

import (
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := auth.HashPassword("mysecret")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if hash == "mysecret" {
		t.Error("hash should not equal plaintext")
	}
	if !auth.CheckPassword(hash, "mysecret") {
		t.Error("CheckPassword should return true for correct password")
	}
	if auth.CheckPassword(hash, "wrongpassword") {
		t.Error("CheckPassword should return false for wrong password")
	}
}

func TestCreateAndGetSession(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Get admin user ID
	var userID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)

	sessionID, err := auth.CreateSession(db, userID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if sessionID == "" {
		t.Fatal("session ID should not be empty")
	}
	if len(sessionID) != 64 { // 32 bytes hex encoded
		t.Errorf("expected session ID length 64, got %d", len(sessionID))
	}

	// Retrieve session
	gotUserID, err := auth.GetSessionUserID(db, sessionID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if gotUserID != userID {
		t.Errorf("expected user ID %d, got %d", userID, gotUserID)
	}
}

func TestGetSession_Invalid(t *testing.T) {
	db := testutil.SetupTestDB(t)

	_, err := auth.GetSessionUserID(db, "nonexistent-session-id")
	if err == nil {
		t.Fatal("expected error for invalid session")
	}
}

func TestTouchSession_SlidesIdleWindow(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var userID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)
	sessionID, _ := auth.CreateSession(db, userID)

	// Move expires_at into the near past of the sliding window — simulate
	// a session that's been alive for ~7h of its 10h idle budget.
	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.DateTime)
	if _, err := db.Exec("UPDATE sessions SET expires_at = ? WHERE id = ?", past, sessionID); err != nil {
		t.Fatalf("rewind expires_at: %v", err)
	}

	if err := auth.TouchSession(db, sessionID); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	var got string
	if err := db.QueryRow("SELECT expires_at FROM sessions WHERE id = ?", sessionID).Scan(&got); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}
	gotT, err := time.Parse(time.DateTime, got)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	// Should now be well into the future (close to +10h).
	if time.Until(gotT) < 8*time.Hour {
		t.Errorf("expected expires_at pushed ~10h ahead, got %s (in %s)", got, time.Until(gotT))
	}
}

func TestTouchSession_RespectsAbsoluteCap(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var userID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)
	sessionID, _ := auth.CreateSession(db, userID)

	// Shrink the absolute cap so the session has only 30 min left no matter
	// what TouchSession tries to do.
	cap := time.Now().UTC().Add(30 * time.Minute).Format(time.DateTime)
	if _, err := db.Exec("UPDATE sessions SET absolute_expires_at = ? WHERE id = ?", cap, sessionID); err != nil {
		t.Fatalf("shrink absolute cap: %v", err)
	}

	if err := auth.TouchSession(db, sessionID); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	var got string
	if err := db.QueryRow("SELECT expires_at FROM sessions WHERE id = ?", sessionID).Scan(&got); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}
	if got != cap {
		t.Errorf("expected expires_at clamped to absolute cap %q, got %q", cap, got)
	}
}

func TestGetSession_RejectedPastAbsoluteCap(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var userID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)
	sessionID, _ := auth.CreateSession(db, userID)

	// expires_at still in the future but absolute cap already passed.
	past := time.Now().UTC().Add(-1 * time.Minute).Format(time.DateTime)
	if _, err := db.Exec("UPDATE sessions SET absolute_expires_at = ? WHERE id = ?", past, sessionID); err != nil {
		t.Fatalf("expire absolute cap: %v", err)
	}

	if _, err := auth.GetSession(db, sessionID); err == nil {
		t.Error("expected GetSession to fail once absolute cap has passed")
	}
}

func TestDeleteSession(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var userID int
	db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID)

	sessionID, _ := auth.CreateSession(db, userID)

	if err := auth.DeleteSession(db, sessionID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Session should no longer be valid
	_, err := auth.GetSessionUserID(db, sessionID)
	if err == nil {
		t.Error("expected error after session deletion")
	}
}
