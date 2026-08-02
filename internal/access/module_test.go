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
	actor := access.Actor{UserID: 1, CanManageUsers: true, CanManageRoles: true}
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
	actor := access.Actor{UserID: 1, CanManageUsers: true, CanManageRoles: true}

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
