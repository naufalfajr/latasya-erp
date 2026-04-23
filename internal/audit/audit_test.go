package audit_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/naufal/latasya-erp/internal/audit"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestLog_BasicOK(t *testing.T) {
	db := testutil.SetupTestDB(t)

	ctx := context.Background()
	audit.Log(ctx, db, audit.Event{
		Action:      "invoice.create",
		TargetType:  "invoice",
		TargetID:    42,
		TargetLabel: "INV-2026-001",
		Metadata:    map[string]any{"total": 1500000, "contact_id": 7},
	})

	var action, targetType, targetLabel, result, metadata string
	var targetID int64
	err := db.QueryRow(`
		SELECT action, target_type, target_id, target_label, result, metadata
		FROM audit_log ORDER BY id DESC LIMIT 1`).
		Scan(&action, &targetType, &targetID, &targetLabel, &result, &metadata)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if action != "invoice.create" {
		t.Errorf("action = %q, want %q", action, "invoice.create")
	}
	if targetType != "invoice" || targetID != 42 || targetLabel != "INV-2026-001" {
		t.Errorf("target mismatch: type=%q id=%d label=%q", targetType, targetID, targetLabel)
	}
	if result != "ok" {
		t.Errorf("result = %q, want ok", result)
	}
	if metadata == "" {
		t.Errorf("metadata should be populated, got empty")
	}
}

func TestLog_WithError_SetsFailResult(t *testing.T) {
	db := testutil.SetupTestDB(t)

	audit.Log(context.Background(), db, audit.Event{
		Action:        "auth.login_failed",
		ActorUsername: "bob",
		Err:           errors.New("bad password"),
	})

	var result, errMsg, actor string
	err := db.QueryRow(`
		SELECT result, COALESCE(error_message, ''), COALESCE(actor_username, '')
		FROM audit_log ORDER BY id DESC LIMIT 1`).
		Scan(&result, &errMsg, &actor)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result != "fail" {
		t.Errorf("result = %q, want fail", result)
	}
	if errMsg != "bad password" {
		t.Errorf("error_message = %q, want %q", errMsg, "bad password")
	}
	if actor != "bob" {
		t.Errorf("actor_username = %q, want bob", actor)
	}
}

func TestLog_InsertFailureIsSwallowed(t *testing.T) {
	db := testutil.SetupTestDB(t)
	db.Close() // force subsequent insert to fail

	// Should not panic or surface an error — the contract is fire-and-forget.
	audit.Log(context.Background(), db, audit.Event{Action: "test.nop"})
}

func TestDiff_OnlyChangedFields(t *testing.T) {
	old := map[string]any{"name": "Old Name", "status": "draft", "total": 100}
	new := map[string]any{"name": "New Name", "status": "draft", "total": 150}

	got := audit.Diff(old, new, []string{"name", "status", "total"})
	want := map[string]any{
		"before": map[string]any{"name": "Old Name", "total": 100},
		"after":  map[string]any{"name": "New Name", "total": 150},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Diff mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDiff_NoChanges_ReturnsNil(t *testing.T) {
	old := map[string]any{"name": "same", "status": "draft"}
	new := map[string]any{"name": "same", "status": "draft"}

	got := audit.Diff(old, new, []string{"name", "status"})
	if got != nil {
		t.Errorf("expected nil for unchanged rows, got %#v", got)
	}
}

func TestDiff_UnlistedFieldsAreIgnored(t *testing.T) {
	// password_hash is deliberately not in the fields list, so even though it
	// changed, it must not appear in the diff.
	old := map[string]any{"name": "a", "password_hash": "old-hash"}
	new := map[string]any{"name": "a", "password_hash": "NEW-SECRET-HASH"}

	got := audit.Diff(old, new, []string{"name"})
	if got != nil {
		t.Errorf("expected nil diff (only password_hash changed, which is unlisted), got %#v", got)
	}
}
