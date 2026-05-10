package model_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

var tokenFormatRE = regexp.MustCompile(`^lat_[0-9A-Za-z]{32}$`)

func TestGenerateAPIToken_Format(t *testing.T) {
	plaintext, prefix, hash, err := model.GenerateAPIToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !tokenFormatRE.MatchString(plaintext) {
		t.Errorf("plaintext %q does not match expected format", plaintext)
	}
	if prefix != plaintext[:8] {
		t.Errorf("prefix %q is not first 8 chars of plaintext %q", prefix, plaintext)
	}
	if len(hash) != 64 {
		t.Errorf("hash length %d, want 64", len(hash))
	}

	h := sha256.Sum256([]byte(plaintext))
	want := hex.EncodeToString(h[:])
	if hash != want {
		t.Errorf("hash mismatch: got %q, want %q", hash, want)
	}
}

func TestGenerateAPIToken_Uniqueness(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		plaintext, _, _, err := model.GenerateAPIToken()
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if !tokenFormatRE.MatchString(plaintext) {
			t.Errorf("iteration %d: token %q does not match expected format", i, plaintext)
		}
		if _, dup := seen[plaintext]; dup {
			t.Fatalf("duplicate token at iteration %d: %q", i, plaintext)
		}
		seen[plaintext] = struct{}{}
	}
}

func TestCreateAPIToken(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "tokenuser", "pass", "bookkeeper")

	scopes := []string{model.CapInvoicesManage, model.CapReportsView}
	token, plaintext, err := model.CreateAPIToken(db, userID, "my-token", scopes, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	if token.UserID != userID {
		t.Errorf("UserID: got %d, want %d", token.UserID, userID)
	}
	if token.Name != "my-token" {
		t.Errorf("Name: got %q, want %q", token.Name, "my-token")
	}
	if len(token.Scopes) != 2 {
		t.Errorf("Scopes len: got %d, want 2", len(token.Scopes))
	}
	if plaintext == "" || !tokenFormatRE.MatchString(plaintext) {
		t.Errorf("plaintext %q has unexpected format", plaintext)
	}

	h := sha256.Sum256([]byte(plaintext))
	wantHash := hex.EncodeToString(h[:])
	var storedHash string
	err = db.QueryRow("SELECT token_hash FROM api_tokens WHERE id = ?", token.ID).Scan(&storedHash)
	if err != nil {
		t.Fatalf("query stored hash: %v", err)
	}
	if storedHash != wantHash {
		t.Errorf("stored hash mismatch")
	}
}

func TestGetAPITokenByHash_Valid(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "hashuser", "pass", "bookkeeper")

	_, plaintext, err := model.CreateAPIToken(db, userID, "test", []string{model.CapReportsView}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	h := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(h[:])

	found, err := model.GetAPITokenByHash(db, hash)
	if err != nil {
		t.Fatalf("GetAPITokenByHash: %v", err)
	}
	if found.UserID != userID {
		t.Errorf("UserID: got %d, want %d", found.UserID, userID)
	}
}

func TestGetAPITokenByHash_Revoked(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "revokeduser", "pass", "bookkeeper")

	tok, plaintext, err := model.CreateAPIToken(db, userID, "revoke-me", []string{}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	if err := model.RevokeAPIToken(db, userID, tok.ID); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}

	h := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(h[:])
	_, err = model.GetAPITokenByHash(db, hash)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for revoked token, got %v", err)
	}
}

func TestGetAPITokenByHash_Expired(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "expireduser", "pass", "bookkeeper")

	past := time.Now().Add(-time.Hour)
	_, plaintext, err := model.CreateAPIToken(db, userID, "expired", []string{}, &past)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	h := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(h[:])
	_, err = model.GetAPITokenByHash(db, hash)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for expired token, got %v", err)
	}
}

func TestGetAPITokenByHash_WrongHash(t *testing.T) {
	db := testutil.SetupTestDB(t)

	_, err := model.GetAPITokenByHash(db, "0000000000000000000000000000000000000000000000000000000000000000")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for unknown hash, got %v", err)
	}
}

func TestListAPITokensByUser(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userA := testutil.CreateTestUser(t, db, "userA", "pass", "bookkeeper")
	userB := testutil.CreateTestUser(t, db, "userB", "pass", "bookkeeper")

	if _, _, err := model.CreateAPIToken(db, userA, "token-a1", []string{}, nil); err != nil {
		t.Fatalf("create token a1: %v", err)
	}
	if _, _, err := model.CreateAPIToken(db, userA, "token-a2", []string{}, nil); err != nil {
		t.Fatalf("create token a2: %v", err)
	}
	if _, _, err := model.CreateAPIToken(db, userB, "token-b1", []string{}, nil); err != nil {
		t.Fatalf("create token b1: %v", err)
	}

	tokens, err := model.ListAPITokensByUser(db, userA)
	if err != nil {
		t.Fatalf("ListAPITokensByUser: %v", err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens for userA, got %d", len(tokens))
	}
	for _, tok := range tokens {
		if tok.UserID != userA {
			t.Errorf("unexpected UserID %d in listing for userA", tok.UserID)
		}
	}
}

func TestRevokeAPIToken(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "revoker", "pass", "bookkeeper")
	otherID := testutil.CreateTestUser(t, db, "other", "pass", "bookkeeper")

	tok, _, err := model.CreateAPIToken(db, userID, "to-revoke", []string{}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	if err := model.RevokeAPIToken(db, userID, tok.ID); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	if err := model.RevokeAPIToken(db, userID, tok.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("second revoke: expected sql.ErrNoRows, got %v", err)
	}

	tok2, _, err := model.CreateAPIToken(db, userID, "other-owners-token", []string{}, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if err := model.RevokeAPIToken(db, otherID, tok2.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("wrong user revoke: expected sql.ErrNoRows, got %v", err)
	}
}

func TestScopesRoundTrip(t *testing.T) {
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, "scopeuser", "pass", "bookkeeper")

	want := []string{model.CapAccountsManage, model.CapInvoicesManage}
	tok, plaintext, err := model.CreateAPIToken(db, userID, "scoped", want, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	h := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(h[:])
	found, err := model.GetAPITokenByHash(db, hash)
	if err != nil {
		t.Fatalf("GetAPITokenByHash: %v", err)
	}

	if len(found.Scopes) != len(want) {
		t.Fatalf("scopes len: got %d, want %d", len(found.Scopes), len(want))
	}
	for i, s := range found.Scopes {
		if s != want[i] {
			t.Errorf("scopes[%d]: got %q, want %q", i, s, want[i])
		}
	}

	list, err := model.ListAPITokensByUser(db, userID)
	if err != nil {
		t.Fatalf("ListAPITokensByUser: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("expected at least one token in list")
	}
	_ = tok
}
