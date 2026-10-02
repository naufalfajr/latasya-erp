package apitoken

import (
	"context"
	"errors"
	"testing"

	latasyaerp "github.com/naufal/latasya-erp"
	"github.com/naufal/latasya-erp/internal/database"
	"github.com/naufal/latasya-erp/internal/model"
)

// RevokeAny approves the owner's role before revoke's transaction; if the owner
// was promoted in between, revoke must refuse rather than act on the stale check.
func TestRevokeRefusesWhenOwnerRoleChanged(t *testing.T) {
	// Same setup as testutil.SetupTestDB, which this package can't import (cycle).
	database.SetMigrations(latasyaerp.MigrationFS)
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := database.Seed(db); err != nil {
		t.Fatal(err)
	}
	module := New(db)
	ctx := context.Background()
	created, err := module.Create(ctx, Actor{UserID: 1, Username: "admin", IsAdmin: true}, Draft{Name: "ops", Scopes: []string{model.CapReportsView}})
	if err != nil {
		t.Fatal(err)
	}
	manager := Actor{UserID: 1, CanManageUsers: true}

	if _, err := module.revoke(ctx, manager, 1, "hr", created.Token.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("stale approved role: error=%v, want ErrForbidden", err)
	}
	if _, err := module.Authenticate(ctx, created.Plaintext); err != nil {
		t.Errorf("token revoked despite the role change: %v", err)
	}
	if _, err := module.revoke(ctx, manager, 1, model.RoleAdmin, created.Token.ID); err != nil {
		t.Errorf("matching approved role: %v", err)
	}
}
