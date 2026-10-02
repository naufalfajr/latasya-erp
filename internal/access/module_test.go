package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/naufal/latasya-erp/internal/access"
	"github.com/naufal/latasya-erp/internal/auth"
	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestUserLifecycleAndRoleProtection(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := access.New(db, auth.HashPassword)
	ctx := context.Background()
	actor := access.Actor{UserID: 1, CanManageUsers: true, CanManageRoles: true, IsAdmin: true}
	role, err := module.CreateRole(ctx, actor, access.RoleDraft{Name: "dispatcher", Capabilities: []string{model.CapInvoicesManage}})
	if err != nil {
		t.Fatal(err)
	}
	user, err := module.CreateUser(ctx, actor, access.UserDraft{Username: "operator", FullName: "Operator", Role: role.Name, IsActive: true, Password: "test"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := module.LookupUserForAuth(ctx, "operator")
	if err != nil || !auth.CheckPassword(loaded.Password, "test") {
		t.Fatalf("auth lookup failed: %v", err)
	}
	if _, err := module.DeleteRole(ctx, actor, role.Name); err == nil {
		t.Fatal("assigned role deletion should fail")
	}
	if _, err := module.DeactivateUser(ctx, access.Actor{UserID: user.ID, CanManageUsers: true}, user.ID); err == nil {
		t.Fatal("self deactivation should fail")
	}
}

func TestUserCreateInvalidRoleIsAtomic(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := access.New(db, auth.HashPassword)
	_, err := module.CreateUser(context.Background(), access.Actor{UserID: 1, CanManageUsers: true}, access.UserDraft{Username: "nobody", FullName: "Nobody", Role: "missing", IsActive: true, Password: "password"})
	var validation *access.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error=%v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username='nobody'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestAdminQueriesRequireAuthorization(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := access.New(db, auth.HashPassword)
	ctx := context.Background()

	checks := []struct {
		name string
		call func() error
	}{
		{"get user", func() error { _, err := module.GetUser(ctx, access.Actor{}, 1); return err }},
		{"list users", func() error { _, err := module.ListUsers(ctx, access.Actor{}, access.ListFilter{}); return err }},
		{"get role", func() error { _, err := module.GetRole(ctx, access.Actor{}, model.RoleAdmin); return err }},
		{"list roles", func() error { _, err := module.ListRoles(ctx, access.Actor{}, access.ListFilter{}); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, access.ErrForbidden) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestUserAndRoleAdministration(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := access.New(db, auth.HashPassword)
	ctx := context.Background()
	actor := access.Actor{UserID: 1, CanManageUsers: true, CanManageRoles: true, IsAdmin: true}

	role, err := module.CreateRole(ctx, actor, access.RoleDraft{Name: "operations", Description: "Operations", Capabilities: []string{model.CapInvoicesManage}})
	if err != nil {
		t.Fatal(err)
	}
	updatedRole, err := module.UpdateRole(ctx, actor, role.Name, access.RoleDraft{Description: "Dispatch", Capabilities: []string{model.CapContactsManage}})
	if err != nil || updatedRole.Description != "Dispatch" {
		t.Fatalf("role=%v err=%v", updatedRole, err)
	}

	user, err := module.CreateUser(ctx, actor, access.UserDraft{Username: "driver", FullName: "Driver", Role: role.Name, IsActive: true, Password: "password1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := module.ListUsers(ctx, actor, access.ListFilter{Limit: 1, Offset: 1})
	if err != nil || len(result.Users) != 1 || result.Total < 2 {
		t.Fatalf("result=%v err=%v", result, err)
	}
	user, err = module.UpdateUser(ctx, actor, user.ID, access.UserDraft{FullName: "Lead Driver", Role: model.RoleViewer, IsActive: true, Password: "password2"})
	if err != nil || user.FullName != "Lead Driver" || !user.MustChangePassword {
		t.Fatalf("user=%v err=%v", user, err)
	}
	authUser, err := module.LookupUserByIDForAuth(ctx, user.ID)
	if err != nil || !auth.CheckPassword(authUser.Password, "password2") {
		t.Fatalf("auth user=%v err=%v", authUser, err)
	}
	if _, err := module.DeactivateUser(ctx, actor, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := module.DeleteRole(ctx, actor, role.Name); err != nil {
		t.Fatal(err)
	}
}

func TestCanAssign(t *testing.T) {
	all := model.AllCapabilities
	cases := []struct {
		name  string
		actor access.Actor
		role  model.Role
		want  bool
	}{
		{"admin assigns admin", access.Actor{IsAdmin: true}, model.Role{Name: model.RoleAdmin, Capabilities: all}, true},
		{"admin assigns anything", access.Actor{IsAdmin: true}, model.Role{Name: "x", Capabilities: all}, true},
		{"non-admin never assigns admin, even holding everything", access.Actor{Capabilities: all}, model.Role{Name: model.RoleAdmin}, false},
		{"subset of own capabilities", access.Actor{Capabilities: []string{model.CapUsersManage, model.CapReportsView}}, model.Role{Name: "x", Capabilities: []string{model.CapReportsView}}, true},
		{"role with no capabilities", access.Actor{}, model.Role{Name: "x"}, true},
		{"one capability beyond own", access.Actor{Capabilities: []string{model.CapUsersManage}}, model.Role{Name: "x", Capabilities: []string{model.CapUsersManage, model.CapJournalsManage}}, false},
	}
	for _, c := range cases {
		if got := c.actor.CanAssign(c.role); got != c.want {
			t.Errorf("%s: CanAssign = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestUserManagerCannotEscalate(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := access.New(db, auth.HashPassword)
	ctx := context.Background()
	admin := access.Actor{UserID: 1, CanManageUsers: true, CanManageRoles: true, IsAdmin: true}
	for _, role := range []access.RoleDraft{
		{Name: "hr", Capabilities: []string{model.CapUsersManage}},
		{Name: "accountant", Capabilities: []string{model.CapJournalsManage}},
	} {
		if _, err := module.CreateRole(ctx, admin, role); err != nil {
			t.Fatal(err)
		}
	}
	managerID := testutil.CreateTestUser(t, db, "manager", "password", "hr")
	accountantID := testutil.CreateTestUser(t, db, "acct", "password", "accountant")
	manager := access.Actor{UserID: managerID, CanManageUsers: true, Capabilities: []string{model.CapUsersManage}}
	roleOf := func(id int) string {
		var role string
		db.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&role)
		return role
	}
	outsidePermissions := func(err error) bool {
		var validation *access.ValidationError
		return errors.As(err, &validation) && validation.Fields["role"] == "outside your capabilities"
	}

	// Handing out a role beyond one's own is a validation error on the role field.
	for _, role := range []string{model.RoleAdmin, "accountant"} {
		if _, err := module.CreateUser(ctx, manager, access.UserDraft{Username: "new-" + role, FullName: "New", Role: role, IsActive: true, Password: "password"}); !outsidePermissions(err) {
			t.Errorf("create %s user: error=%v, want role outside your capabilities", role, err)
		}
		if _, err := module.UpdateUser(ctx, manager, managerID, access.UserDraft{FullName: "Manager", Role: role, IsActive: true}); !outsidePermissions(err) {
			t.Errorf("promote self to %s: error=%v, want role outside your capabilities", role, err)
		}
	}
	if roleOf(managerID) != "hr" {
		t.Fatalf("manager role changed to %q", roleOf(managerID))
	}

	// Users with more power than the manager can't be edited, reset or deactivated.
	var adminHash string
	db.QueryRow(`SELECT password FROM users WHERE id=1`).Scan(&adminHash)
	for _, id := range []int{1, accountantID} {
		if _, err := module.UpdateUser(ctx, manager, id, access.UserDraft{FullName: "Taken", Role: "hr", IsActive: true, Password: "owned1234"}); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("update user %d: error=%v, want ErrForbidden", id, err)
		}
		if _, err := module.DeactivateUser(ctx, manager, id); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("deactivate user %d: error=%v, want ErrForbidden", id, err)
		}
	}
	var hash string
	var active bool
	db.QueryRow(`SELECT password, is_active FROM users WHERE id=1`).Scan(&hash, &active)
	if hash != adminHash || !active || roleOf(1) != model.RoleAdmin {
		t.Error("admin account was changed by a non-admin user manager")
	}

	// Within their own permissions the manager still manages users normally.
	peer, err := module.CreateUser(ctx, manager, access.UserDraft{Username: "peer", FullName: "Peer", Role: "hr", IsActive: true, Password: "password"})
	if err != nil {
		t.Fatalf("create peer: %v", err)
	}
	if _, err := module.UpdateUser(ctx, manager, peer.ID, access.UserDraft{FullName: "Peer Two", Role: model.RoleViewer, IsActive: true}); err == nil {
		t.Error("viewer role carries reports.view, which the manager lacks; want a refusal")
	}
	if _, err := module.DeactivateUser(ctx, manager, peer.ID); err != nil {
		t.Errorf("deactivate peer: %v", err)
	}

	// Admins keep full control, including granting admin.
	if _, err := module.UpdateUser(ctx, admin, managerID, access.UserDraft{FullName: "Manager", Role: model.RoleAdmin, IsActive: true}); err != nil {
		t.Errorf("admin promoting a user: %v", err)
	}
}

func TestManageabilityEdgeCases(t *testing.T) {
	db := testutil.SetupTestDB(t)
	module := access.New(db, auth.HashPassword)
	ctx := context.Background()
	manager := access.Actor{UserID: 1, CanManageUsers: true, Capabilities: []string{model.CapUsersManage}}
	ghostID := testutil.CreateTestUser(t, db, "ghost", "password", "ghostrole")

	if _, err := module.UpdateUser(ctx, manager, 99999, access.UserDraft{FullName: "X", Role: model.RoleViewer, IsActive: true}); !errors.Is(err, access.ErrNotFound) {
		t.Errorf("update unknown user: error=%v, want ErrNotFound", err)
	}
	// A role that can't be checked is refused to non-admins, never assumed safe.
	if _, err := module.DeactivateUser(ctx, manager, ghostID); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("non-admin deactivating a user with a missing role: error=%v, want ErrForbidden", err)
	}
	if err := module.CheckManageable(ctx, access.Actor{UserID: 1, Capabilities: []string{model.CapUsersManage}}, model.RoleViewer); !errors.Is(err, access.ErrForbidden) {
		t.Errorf("CheckManageable without users.manage: error=%v, want ErrForbidden", err)
	}
}
