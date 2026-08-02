package idempotency_test

import (
	"context"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/idempotency"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestCleanExpiredIdempotencyKeys(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID); err != nil {
		t.Fatalf("get admin: %v", err)
	}

	expiredKey := "expired-key"
	past := time.Now().UTC().Add(-25 * time.Hour).Format(time.DateTime)
	_, err := db.Exec(`
		INSERT INTO idempotency_keys (key, user_id, request_hash, response_status, response_body, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, expiredKey, userID, "hash", 200, []byte("{}"), past)
	if err != nil {
		t.Fatalf("insert expired key: %v", err)
	}

	liveKey := "live-key"
	store := idempotency.New(db)
	if err := store.Save(context.Background(), liveKey, userID, "hash2", 201, []byte("{}")); err != nil {
		t.Fatalf("store live key: %v", err)
	}

	store.CleanExpired(context.Background())

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM idempotency_keys WHERE key = ?", expiredKey).Scan(&count); err != nil {
		t.Fatalf("query expired key: %v", err)
	}
	if count != 0 {
		t.Errorf("expected expired key removed, got count %d", count)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM idempotency_keys WHERE key = ?", liveKey).Scan(&count); err != nil {
		t.Fatalf("query live key: %v", err)
	}
	if count != 1 {
		t.Errorf("expected live key to remain, got count %d", count)
	}
}

// TestLookupIdempotency_ExpiresSameCalendarDay guards a regression where
// expires_at was written in RFC3339 ("...T...Z") but compared against
// SQLite's datetime('now') ("... ...", space separator): 'T' sorts after
// ' ', so a same-day expiry that had already passed was never detected.
func TestLookupIdempotency_ExpiresSameCalendarDay(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)

	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID); err != nil {
		t.Fatalf("get admin: %v", err)
	}

	key := "same-day-expired"
	justExpired := time.Now().UTC().Add(-1 * time.Minute).Format(time.DateTime)
	if _, err := db.Exec(`
		INSERT INTO idempotency_keys (key, user_id, request_hash, response_status, response_body, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, key, userID, "hash", 200, []byte("{}"), justExpired); err != nil {
		t.Fatalf("insert expired key: %v", err)
	}

	rec, err := idempotency.New(db).Lookup(context.Background(), key, userID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected expired same-day record to be treated as missing, got %+v", rec)
	}
}

func TestSaveReplacesExpiredKey(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()
	store := idempotency.New(db)
	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO idempotency_keys
		(key,user_id,request_hash,response_status,response_body,expires_at)
		VALUES (?,?,?,?,?,?)`, "reused", userID, "old", 200, []byte(`{"old":true}`), time.Now().UTC().Add(-time.Minute).Format(time.DateTime)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, "reused", userID, "new", 201, []byte(`{"new":true}`)); err != nil {
		t.Fatal(err)
	}
	record, err := store.Lookup(ctx, "reused", userID)
	if err != nil || record == nil || record.RequestHash != "new" || record.ResponseStatus != 201 || string(record.ResponseBody) != `{"new":true}` {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}
