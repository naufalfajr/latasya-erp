package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestListRoles_SeededDefaults(t *testing.T) {
	db := testutil.SetupTestDB(t)

	roles, err := model.ListRoles(db)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}

	want := map[string]bool{"admin": false, "bookkeeper": false, "viewer": false}
	for _, r := range roles {
		if _, ok := want[r.Name]; ok {
			want[r.Name] = true
			if !r.IsSystem {
				t.Errorf("%s should be system role", r.Name)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected seeded role %q", name)
		}
	}
}

func TestGetRoleByName_Bookkeeper(t *testing.T) {
	db := testutil.SetupTestDB(t)

	r, err := model.GetRoleByName(db, "bookkeeper")
	if err != nil {
		t.Fatalf("get bookkeeper: %v", err)
	}

	expected := []string{
		model.CapContactsManage,
		model.CapJournalsManage,
		model.CapIncomeManage,
		model.CapExpensesManage,
		model.CapInvoicesManage,
		model.CapBillsManage,
		model.CapReportsView,
	}
	for _, cap := range expected {
		if !r.HasCapability(cap) {
			t.Errorf("bookkeeper should have %s", cap)
		}
	}
	for _, cap := range []string{model.CapAccountsManage, model.CapUsersManage, model.CapRolesManage} {
		if r.HasCapability(cap) {
			t.Errorf("bookkeeper should NOT have %s", cap)
		}
	}
}

func TestRoleHasCapability_AdminAlwaysTrue(t *testing.T) {
	db := testutil.SetupTestDB(t)

	r, err := model.GetRoleByName(db, "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}

	for _, cap := range model.AllCapabilities {
		if !r.HasCapability(cap) {
			t.Errorf("admin should have %s", cap)
		}
	}
	if !r.HasCapability("completely.made.up") {
		t.Error("admin should short-circuit to true for unknown capability")
	}
}

func TestCreateRole_RoundTrip(t *testing.T) {
	db := testutil.SetupTestDB(t)

	newRole := &model.Role{
		Name:         "manager",
		Description:  "Operations manager",
		IsSystem:     false,
		Capabilities: []string{model.CapInvoicesManage, model.CapBillsManage, model.CapReportsView},
	}
	if err := model.CreateRole(db, newRole); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := model.GetRoleByName(db, "manager")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Description != "Operations manager" {
		t.Errorf("description: got %q", got.Description)
	}
	if len(got.Capabilities) != 3 {
		t.Errorf("caps: expected 3, got %d", len(got.Capabilities))
	}
	if got.IsSystem {
		t.Error("new role should not be system")
	}
}

func TestUpdateRole_ReplacesCapabilities(t *testing.T) {
	db := testutil.SetupTestDB(t)

	newRole := &model.Role{Name: "auditor", Capabilities: []string{model.CapReportsView}}
	if err := model.CreateRole(db, newRole); err != nil {
		t.Fatalf("create: %v", err)
	}

	newRole.Capabilities = []string{model.CapReportsView, model.CapInvoicesManage}
	newRole.Description = "Updated"
	if err := model.UpdateRole(db, newRole); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := model.GetRoleByName(db, "auditor")
	if len(got.Capabilities) != 2 || got.Description != "Updated" {
		t.Errorf("unexpected state: %+v", got)
	}
}

func TestDeleteRole(t *testing.T) {
	db := testutil.SetupTestDB(t)

	if err := model.CreateRole(db, &model.Role{Name: "temp"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := model.DeleteRole(db, "temp"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := model.GetRoleByName(db, "temp"); err == nil {
		t.Error("expected not found after delete")
	}
}

func TestCountUsersWithRole(t *testing.T) {
	db := testutil.SetupTestDB(t)

	n, err := model.CountUsersWithRole(db, "admin")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 admin user seeded, got %d", n)
	}

	testutil.CreateTestUser(t, db, "bk1", "pw", "bookkeeper")
	testutil.CreateTestUser(t, db, "bk2", "pw", "bookkeeper")

	n, _ = model.CountUsersWithRole(db, "bookkeeper")
	if n != 2 {
		t.Errorf("expected 2 bookkeepers, got %d", n)
	}
}

func TestUserHasCapability_AdminAlwaysTrue(t *testing.T) {
	u := &model.User{Role: model.RoleAdmin}
	if !u.HasCapability("anything.at.all") {
		t.Error("admin user should always have any capability")
	}
}

func TestUserHasCapability_FromList(t *testing.T) {
	u := &model.User{
		Role:         "custom",
		Capabilities: []string{model.CapInvoicesManage},
	}
	if !u.HasCapability(model.CapInvoicesManage) {
		t.Error("should have invoices.manage")
	}
	if u.HasCapability(model.CapBillsManage) {
		t.Error("should not have bills.manage")
	}
}

func TestUserHasCapability_NilReceiver(t *testing.T) {
	var u *model.User
	if u.HasCapability(model.CapInvoicesManage) {
		t.Error("nil user should not have any capability")
	}
}

func TestRoleHasCapability_NilReceiver(t *testing.T) {
	var r *model.Role
	if r.HasCapability(model.CapInvoicesManage) {
		t.Error("nil role should not have any capability")
	}
}
