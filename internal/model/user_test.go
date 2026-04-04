package model_test

import (
	"testing"

	"github.com/naufal/latasya-erp/internal/model"
	"github.com/naufal/latasya-erp/internal/testutil"
)

func TestGetUserByUsername(t *testing.T) {
	db := testutil.SetupTestDB(t)

	user, err := model.GetUserByUsername(db, "admin")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user.Username != "admin" {
		t.Errorf("expected username 'admin', got %q", user.Username)
	}
	if user.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", user.Role)
	}
	if !user.IsActive {
		t.Error("expected user to be active")
	}
}

func TestGetUserByUsername_NotFound(t *testing.T) {
	db := testutil.SetupTestDB(t)

	_, err := model.GetUserByUsername(db, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestGetUserByID(t *testing.T) {
	db := testutil.SetupTestDB(t)

	// Get admin by username first to find the ID
	admin, _ := model.GetUserByUsername(db, "admin")

	user, err := model.GetUserByID(db, admin.ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if user.Username != "admin" {
		t.Errorf("expected username 'admin', got %q", user.Username)
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	db := testutil.SetupTestDB(t)

	_, err := model.GetUserByID(db, 99999)
	if err == nil {
		t.Fatal("expected error for nonexistent user ID")
	}
}

func TestUserIsAdmin(t *testing.T) {
	admin := &model.User{Role: "admin"}
	viewer := &model.User{Role: "viewer"}

	if !admin.IsAdmin() {
		t.Error("admin user should return true for IsAdmin()")
	}
	if viewer.IsAdmin() {
		t.Error("viewer user should return false for IsAdmin()")
	}
}
