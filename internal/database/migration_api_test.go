package database_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestMigration_APITokensSchema(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Verify api_tokens table exists with expected columns
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='api_tokens'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("api_tokens table not found: err=%v count=%d", err, count)
	}

	// Verify idempotency_keys table exists
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='idempotency_keys'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("idempotency_keys table not found: err=%v count=%d", err, count)
	}

	// Verify actor_token_id column exists on audit_log
	rows, err := db.Query(`PRAGMA table_info(audit_log)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "actor_token_id" {
			found = true
		}
	}
	if !found {
		t.Fatal("actor_token_id column not found in audit_log")
	}

	// Verify indexes exist
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_api_tokens_hash'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("idx_api_tokens_hash index not found: err=%v count=%d", err, count)
	}
}
