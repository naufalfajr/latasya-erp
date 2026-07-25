package auth_test

import (
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/testutil"
)

// TestCleanExpiredSessionsOnce covers the one-shot deletion logic that
// CleanExpiredSessions (an infinite background loop, not directly callable
// from a test) delegates to on each tick.
func TestCleanExpiredSessionsOnce(t *testing.T) {
	db := testutil.SetupTestDB(t)

	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID); err != nil {
		t.Fatalf("get admin: %v", err)
	}

	expiredID, err := auth.CreateSession(db, userID)
	if err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.DateTime)
	if _, err := db.Exec("UPDATE sessions SET expires_at = ? WHERE id = ?", past, expiredID); err != nil {
		t.Fatalf("expire session: %v", err)
	}

	liveID, err := auth.CreateSession(db, userID)
	if err != nil {
		t.Fatalf("create live session: %v", err)
	}

	auth.CleanExpiredSessionsOnce(db)

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", expiredID).Scan(&count); err != nil {
		t.Fatalf("query expired session: %v", err)
	}
	if count != 0 {
		t.Errorf("expected expired session removed, got count %d", count)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", liveID).Scan(&count); err != nil {
		t.Fatalf("query live session: %v", err)
	}
	if count != 1 {
		t.Errorf("expected live session to remain, got count %d", count)
	}
}
