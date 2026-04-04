package auth_test

import (
	"testing"

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
