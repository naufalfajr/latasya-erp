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

func TestMigration_ContactDistancePricingSchema(t *testing.T) {
	db := testutil.SetupTestDB(t)

	rows, err := db.Query(`PRAGMA table_info(contacts)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	columns := map[string]string{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = typ
	}

	for _, name := range []string{"distance_km", "has_sibling_discount", "is_return_only"} {
		if columns[name] == "" {
			t.Fatalf("%s column not found in contacts", name)
		}
	}
	if columns["distance_km"] != "REAL" {
		t.Fatalf("distance_km type: got %q want REAL", columns["distance_km"])
	}
	if columns["price"] != "" {
		t.Fatal("price column should not exist in contacts")
	}

	for _, name := range []string{"idx_contacts_type_active", "idx_contacts_route_id"} {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&count)
		if err != nil || count != 1 {
			t.Fatalf("%s index not found: err=%v count=%d", name, err, count)
		}
	}
}

func TestMigration_SchoolCalendarSchema(t *testing.T) {
	db := testutil.SetupTestDB(t)

	for _, table := range []string{"school_closures", "google_calendar_connections", "google_oauth_states"} {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
		if err != nil || count != 1 {
			t.Fatalf("%s table not found: err=%v count=%d", table, err, count)
		}
	}

	rows, err := db.Query(`PRAGMA table_info(school_closures)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	columns := map[string]string{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		columns[name] = typ
	}

	for _, name := range []string{"source", "title", "start_date", "end_date", "google_event_id"} {
		if columns[name] == "" {
			t.Fatalf("%s column not found in school_closures", name)
		}
	}

	assertColumns := func(table string, names []string) {
		t.Helper()
		rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			t.Fatalf("PRAGMA table_info(%s): %v", table, err)
		}
		defer rows.Close()
		cols := map[string]bool{}
		for rows.Next() {
			var cid int
			var name, typ string
			var notNull, pk int
			var dflt any
			if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
				t.Fatal(err)
			}
			cols[name] = true
		}
		for _, name := range names {
			if !cols[name] {
				t.Fatalf("%s column not found in %s", name, table)
			}
		}
	}
	assertColumns("google_calendar_connections", []string{"id", "calendar_id", "refresh_token", "is_active", "last_sync_at", "last_sync_status", "last_sync_error", "created_at", "updated_at"})
	assertColumns("google_oauth_states", []string{"state", "user_id", "pkce_verifier", "expires_at", "created_at"})

	for _, name := range []string{"idx_school_closures_dates", "idx_school_closures_google_event", "idx_google_oauth_states_expires"} {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&count)
		if err != nil || count != 1 {
			t.Fatalf("%s index not found: err=%v count=%d", name, err, count)
		}
	}
}
