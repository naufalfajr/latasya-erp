package apitoken_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/naufal/latasya-erp/internal/apitoken"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestCreateAuthenticateListRevoke(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := apitoken.New(db)
	ctx := context.Background()
	actor := apitoken.Actor{UserID: 1, Username: "admin", IsAdmin: true}
	created, err := module.Create(ctx, actor, apitoken.Draft{Name: "mcp", Scopes: []string{model.CapReportsView}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Plaintext, "lat_") {
		t.Fatalf("plaintext=%q", created.Plaintext)
	}
	found, err := module.Authenticate(ctx, created.Plaintext)
	if err != nil || found.ID != created.Token.ID {
		t.Fatalf("authenticate=%v err=%v", found, err)
	}
	tokens, err := module.List(ctx, actor)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("tokens=%v err=%v", tokens, err)
	}
	if _, err := module.Revoke(ctx, actor, created.Token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Authenticate(ctx, created.Plaintext); !errors.Is(err, apitoken.ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
}

func TestCreateRejectsScopeOverreach(t *testing.T) {
	db := testutil.SetupTestDB(t)
	_, err := apitoken.New(db).Create(context.Background(), apitoken.Actor{UserID: 1, Capabilities: []string{model.CapReportsView}}, apitoken.Draft{Name: "bad", Scopes: []string{model.CapUsersManage}})
	var validation *apitoken.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error=%v", err)
	}
}

func TestOwnershipExpiryAndNameConflict(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := apitoken.New(db)
	ctx := context.Background()
	owner := apitoken.Actor{UserID: 1, Username: "admin", IsAdmin: true}
	otherID := testutil.CreateTestUser(t, db, "token-owner", "password", model.RoleViewer)
	other := apitoken.Actor{UserID: otherID, Username: "token-owner", IsAdmin: true}

	created, err := module.Create(ctx, owner, apitoken.Draft{Name: "integration", Scopes: []string{model.CapReportsView}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := module.Create(ctx, owner, apitoken.Draft{Name: "integration"}); err == nil {
		t.Fatal("duplicate token name should fail for the same owner")
	} else {
		var conflict *apitoken.ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("error=%v", err)
		}
	}
	if _, err := module.Create(ctx, other, apitoken.Draft{Name: "integration"}); err != nil {
		t.Fatalf("same name for another owner: %v", err)
	}
	ownerTokens, err := module.List(ctx, owner)
	if err != nil || len(ownerTokens) != 1 {
		t.Fatalf("owner tokens=%v err=%v", ownerTokens, err)
	}
	otherTokens, err := module.List(ctx, other)
	if err != nil || len(otherTokens) != 1 || otherTokens[0].UserID != otherID {
		t.Fatalf("other tokens=%v err=%v", otherTokens, err)
	}
	if _, err := module.Revoke(ctx, other, created.Token.ID); !errors.Is(err, apitoken.ErrNotFound) {
		t.Fatalf("cross-owner revoke error=%v", err)
	}
	if _, err := db.Exec(`UPDATE api_tokens SET expires_at=? WHERE id=?`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), created.Token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Authenticate(ctx, created.Plaintext); !errors.Is(err, apitoken.ErrNotFound) {
		t.Fatalf("expired token error=%v", err)
	}
}

func TestAdminStillRejectsUnknownScope(t *testing.T) {
	db := testutil.SetupTestDB(t)
	_, err := apitoken.New(db).Create(context.Background(), apitoken.Actor{UserID: 1, IsAdmin: true}, apitoken.Draft{Name: "bad-admin", Scopes: []string{"not.real"}})
	var validation *apitoken.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error=%v", err)
	}
}
