package apitoken_test

import (
	"context"
	"errors"
	"fmt"
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

func TestListAllAndRevokeAny(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := apitoken.New(db)
	ctx := context.Background()
	staffID := testutil.CreateTestUser(t, db, "staff", "password", model.RoleBookkeeper)
	staff := apitoken.Actor{UserID: staffID, Username: "staff", Capabilities: []string{model.CapIncomeManage}}
	manager := apitoken.Actor{UserID: 1, Username: "admin", IsAdmin: true, CanManageUsers: true}

	first, err := module.Create(ctx, staff, apitoken.Draft{Name: "telegram", Scopes: []string{model.CapIncomeManage}})
	if err != nil {
		t.Fatal(err)
	}

	// No actor at all is always refused.
	anonymous := apitoken.Actor{CanManageUsers: true}
	if _, err := module.ListAll(ctx, anonymous); !errors.Is(err, apitoken.ErrForbidden) {
		t.Errorf("ListAll without a user: error=%v, want ErrForbidden", err)
	}
	if _, err := module.RevokeAny(ctx, anonymous, first.Token.ID); !errors.Is(err, apitoken.ErrForbidden) {
		t.Errorf("RevokeAny without a user: error=%v, want ErrForbidden", err)
	}
	if _, err := module.Revoke(ctx, apitoken.Actor{}, first.Token.ID); !errors.Is(err, apitoken.ErrForbidden) {
		t.Errorf("Revoke without a user: error=%v, want ErrForbidden", err)
	}

	// Without users.manage nobody sees or revokes other users' tokens, admin role or not.
	for _, actor := range []apitoken.Actor{staff, {UserID: 1, Username: "admin", IsAdmin: true}} {
		if _, err := module.ListAll(ctx, actor); !errors.Is(err, apitoken.ErrForbidden) {
			t.Errorf("ListAll as %+v: error=%v, want ErrForbidden", actor, err)
		}
		if _, err := module.RevokeAny(ctx, actor, first.Token.ID); !errors.Is(err, apitoken.ErrForbidden) {
			t.Errorf("RevokeAny as %+v: error=%v, want ErrForbidden", actor, err)
		}
	}

	all, err := module.ListAll(ctx, manager)
	if err != nil || len(all) != 1 || all[0].OwnerUsername != "staff" || all[0].OwnerName == "" || all[0].Name != "telegram" {
		t.Fatalf("ListAll = %+v, err=%v", all, err)
	}

	if _, err := module.RevokeAny(ctx, manager, first.Token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := module.Authenticate(ctx, first.Plaintext); !errors.Is(err, apitoken.ErrNotFound) {
		t.Errorf("revoked token still authenticates: error=%v", err)
	}
	var actor, metadata string
	db.QueryRow(`SELECT actor_username, metadata FROM audit_log WHERE action='api_token.revoke'`).Scan(&actor, &metadata)
	if actor != "admin" || !strings.Contains(metadata, fmt.Sprintf(`"owner_user_id":%d`, staffID)) || !strings.Contains(metadata, `"owner_username":"staff"`) {
		t.Errorf("audit: actor=%q metadata=%s, want admin revoking staff's token", actor, metadata)
	}

	// Active tokens are listed before revoked ones, even older ones.
	second, err := module.Create(ctx, staff, apitoken.Draft{Name: "script", Scopes: []string{model.CapIncomeManage}})
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE api_tokens SET created_at=datetime('now','-1 day') WHERE id=?`, second.Token.ID)
	all, _ = module.ListAll(ctx, manager)
	if len(all) != 2 || all[0].ID != second.Token.ID || all[1].RevokedAt == nil {
		t.Errorf("ListAll order: %+v, want the active token first", all)
	}

	for _, id := range []int{first.Token.ID, 99999} {
		if _, err := module.RevokeAny(ctx, manager, id); !errors.Is(err, apitoken.ErrNotFound) {
			t.Errorf("RevokeAny(%d): error=%v, want ErrNotFound", id, err)
		}
	}

	// Database failures surface as errors, not as "not found" or an empty list.
	db.Close()
	if _, err := module.ListAll(ctx, manager); err == nil {
		t.Error("ListAll on a closed database: want an error")
	}
	if _, err := module.RevokeAny(ctx, manager, second.Token.ID); err == nil || errors.Is(err, apitoken.ErrNotFound) {
		t.Errorf("RevokeAny on a closed database: error=%v, want a database error", err)
	}
	if _, err := module.Revoke(ctx, staff, second.Token.ID); err == nil || errors.Is(err, apitoken.ErrNotFound) {
		t.Errorf("Revoke on a closed database: error=%v, want a database error", err)
	}
}
