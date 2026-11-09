package mcpauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/mcpauth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func deviceHash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// touch starts a handoff and returns the module and db for follow-ups.
func touch(t *testing.T, userCode string) (*mcpauth.Module, *context.Context, string, string) {
	t.Helper()
	db := testutil.SetupTestDB(t)
	m := mcpauth.New(db)
	ctx := context.Background()
	h, err := m.Touch(ctx, deviceHash("device-"+userCode), userCode, "latasya-mcp/1.0 (test)")
	if err != nil {
		t.Fatalf("touch: %v", err)
	}
	if h.Status != "pending" {
		t.Fatalf("fresh handoff status: got %q, want pending", h.Status)
	}
	return m, &ctx, deviceHash("device-" + userCode), userCode
}

func TestTouch_IdempotentWhilePending(t *testing.T) {
	m, ctxP, hash, code := touch(t, "AB12CD34")
	ctx := *ctxP
	h, err := m.Touch(ctx, hash, code, "latasya-mcp/1.0 (test)")
	if err != nil {
		t.Fatalf("second touch: %v", err)
	}
	if h.Status != "pending" {
		t.Fatalf("repeat touch status: got %q, want pending", h.Status)
	}
}

func TestTouch_DeviceRestartedLogin(t *testing.T) {
	m, ctxP, hash, _ := touch(t, "AB12CD34")
	ctx := *ctxP
	if _, err := m.Touch(ctx, hash, "ZZ99ZZ99", "latasya-mcp/1.0 (test)"); !errors.Is(err, mcpauth.ErrGone) {
		t.Fatalf("device hash with new user code: error=%v, want ErrGone", err)
	}
}

func TestApproveAndConsume_MintsWorkingToken(t *testing.T) {
	m, ctxP, hash, code := touch(t, "AB12CD34")
	ctx := *ctxP

	expiry := time.Now().UTC().Add(90 * 24 * time.Hour)
	if err := m.Approve(ctx, code, 1, "admin", []string{model.CapInvoicesManage, model.CapReportsView}, &expiry); err != nil {
		t.Fatalf("approve: %v", err)
	}

	created, h, err := m.Consume(ctx, hash)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if h.Username != "admin" {
		t.Errorf("handoff username: got %q", h.Username)
	}
	if created.Token.UserID != 1 {
		t.Errorf("token owner: got %d, want 1", created.Token.UserID)
	}
	if !strings.HasPrefix(created.Token.Name, "MCP latasya-mcp/1.0 (test) "+time.Now().UTC().Format("2006-01-02")+" ") {
		t.Errorf("token name: got %q", created.Token.Name)
	}
	if len(created.Token.Scopes) != 2 {
		t.Errorf("token scopes: got %v", created.Token.Scopes)
	}
	if created.Plaintext == "" {
		t.Error("plaintext token missing")
	}
	if created.Token.ExpiresAt == nil || !created.Token.ExpiresAt.After(time.Now().Add(89*24*time.Hour)) {
		t.Errorf("token expiry not carried: %v", created.Token.ExpiresAt)
	}

	// Row is gone: a second consume is rejected.
	if _, _, err := m.Consume(ctx, hash); !errors.Is(err, mcpauth.ErrGone) {
		t.Fatalf("second consume: error=%v, want ErrGone", err)
	}
}

func TestConsume_BeforeApprovalIsPending(t *testing.T) {
	m, ctxP, hash, _ := touch(t, "AB12CD34")
	if _, _, err := m.Consume(*ctxP, hash); !errors.Is(err, mcpauth.ErrPending) {
		t.Fatalf("consume before approve: error=%v, want ErrPending", err)
	}
}

func TestDeny_BlocksConsume(t *testing.T) {
	m, ctxP, hash, code := touch(t, "AB12CD34")
	ctx := *ctxP
	if err := m.Deny(ctx, code); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if _, _, err := m.Consume(ctx, hash); !errors.Is(err, mcpauth.ErrDenied) {
		t.Fatalf("consume after deny: error=%v, want ErrDenied", err)
	}
}

func TestApprove_UnknownCodeRejected(t *testing.T) {
	m, ctxP, _, _ := touch(t, "AB12CD34")
	err := m.Approve(*ctxP, "NOPE0000", 1, "admin", []string{model.CapReportsView}, nil)
	if !errors.Is(err, mcpauth.ErrInvalidCode) {
		t.Fatalf("approve unknown code: error=%v, want ErrInvalidCode", err)
	}
}

func TestConsume_ScopeShrunkSinceApproval(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := mcpauth.New(db)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, "shrink-victim", "pw", "viewer")
	_, err := m.Touch(ctx, deviceHash("dev-shrink"), "SHRINK01", "latasya-mcp/1.0 (test)")
	if err != nil {
		t.Fatalf("touch: %v", err)
	}
	// viewer holds only reports.view; approve smuggles in invoices.manage too.
	if err := m.Approve(ctx, "SHRINK01", userID, "shrink-victim",
		[]string{model.CapReportsView, model.CapInvoicesManage}, nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, _, err := m.Consume(ctx, deviceHash("dev-shrink")); !errors.Is(err, mcpauth.ErrScopeChange) {
		t.Fatalf("consume with smuggled scope: error=%v, want ErrScopeChange", err)
	}
}

func TestExpiredHandoff_IsSweptAway(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := mcpauth.New(db)
	ctx := context.Background()

	hash := deviceHash("dev-old")
	if _, err := db.ExecContext(ctx,
		`INSERT INTO mcp_handoffs (device_code_hash, user_code, client, status, expires_at)
		 VALUES (?, 'OLD00001', 'old', 'approved', datetime('now', '-1 minute'))`, hash); err != nil {
		t.Fatalf("seed expired row: %v", err)
	}
	if _, _, err := m.Consume(ctx, hash); !errors.Is(err, mcpauth.ErrGone) {
		t.Fatalf("consume expired: error=%v, want ErrGone (swept)", err)
	}
}

func TestLookup_ForConsentPage(t *testing.T) {
	m, ctxP, _, code := touch(t, "LOOKUP01")
	h, err := m.Lookup(*ctxP, code)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if h.Client != "latasya-mcp/1.0 (test)" || h.Status != "pending" {
		t.Errorf("lookup result: %+v", h)
	}
	if _, err := m.Lookup(*ctxP, "NOPE0000"); !errors.Is(err, mcpauth.ErrGone) {
		t.Errorf("lookup unknown: error=%v, want ErrGone", err)
	}
}

func TestTouch_UserCodeCollisionIsGone(t *testing.T) {
	db := testutil.SetupTestDB(t)
	m := mcpauth.New(db)
	ctx := context.Background()

	if _, err := m.Touch(ctx, deviceHash("device-A"), "SHARED01", "client-A"); err != nil {
		t.Fatalf("first touch: %v", err)
	}
	// A different device reusing an in-flight user code trips UNIQUE(user_code),
	// which the ON CONFLICT(device_code_hash) clause does not cover. The
	// caller must see "gone" (restart login), never an internal error.
	if _, err := m.Touch(ctx, deviceHash("device-B"), "SHARED01", "client-B"); !errors.Is(err, mcpauth.ErrGone) {
		t.Fatalf("user-code collision: error=%v, want ErrGone", err)
	}
}

func TestConsume_SameDayReloginUniqueNames(t *testing.T) {
	// api_tokens has UNIQUE(user_id, name) and soft-only revocation, so a
	// second login for the same user+client on the same day must still mint.
	db := testutil.SetupTestDB(t)
	m := mcpauth.New(db)
	ctx := context.Background()

	for i, code := range []string{"RELOGI01", "RELOGI02"} {
		if _, err := m.Touch(ctx, deviceHash(fmt.Sprintf("relogin-%d", i)), code, "latasya-mcp/1.0 (test)"); err != nil {
			t.Fatalf("touch %d: %v", i, err)
		}
		if err := m.Approve(ctx, code, 1, "admin", []string{model.CapReportsView}, nil); err != nil {
			t.Fatalf("approve %d: %v", i, err)
		}
		created, _, err := m.Consume(ctx, deviceHash(fmt.Sprintf("relogin-%d", i)))
		if err != nil {
			t.Fatalf("consume %d: %v (same-day name collision?)", i, err)
		}
		if created.Token.Name == "" {
			t.Fatal("empty token name")
		}
	}
}
