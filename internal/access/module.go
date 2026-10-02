package access

import (
	"context"
	"database/sql"
	"slices"

	"github.com/naufal/latasya-erp/internal/model"
)

type PasswordHasher func(string) (string, error)

type Module struct {
	db   *sql.DB
	hash PasswordHasher
}

func New(db *sql.DB, hasher PasswordHasher) *Module { return &Module{db: db, hash: hasher} }

type Actor struct {
	UserID         int
	CanManageUsers bool
	CanManageRoles bool
	IsAdmin        bool
	Capabilities   []string
}

// CanAssign reports whether the actor may give this role to a user or manage a
// user who has it: admins any role, others only non-admin roles within their own.
func (a Actor) CanAssign(role model.Role) bool {
	if a.IsAdmin {
		return true
	}
	if role.Name == model.RoleAdmin {
		return false
	}
	for _, capability := range role.Capabilities {
		if !slices.Contains(a.Capabilities, capability) {
			return false
		}
	}
	return true
}

func require(actor Actor, allowed bool) error {
	if actor.UserID <= 0 || !allowed {
		return ErrForbidden
	}
	return nil
}

func requireAssignable(ctx context.Context, q queryer, actor Actor, roleName string) error {
	role, err := getRoleWith(ctx, q, roleName)
	if err == ErrNotFound {
		return &ValidationError{Fields: map[string]string{"role": "invalid role"}}
	}
	if err != nil {
		return err
	}
	if !actor.CanAssign(*role) {
		return &ValidationError{Fields: map[string]string{"role": "outside your capabilities"}}
	}
	return nil
}

// requireManageable keeps user managers away from users with more power than their own.
func requireManageable(ctx context.Context, q queryer, actor Actor, roleName string) error {
	if actor.IsAdmin {
		return nil
	}
	role, err := getRoleWith(ctx, q, roleName)
	if err == ErrNotFound {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if !actor.CanAssign(*role) {
		return ErrForbidden
	}
	return nil
}

// CheckManageable returns ErrForbidden unless the actor may manage users with this
// role, and so their API tokens.
func (m *Module) CheckManageable(ctx context.Context, actor Actor, roleName string) error {
	if err := require(actor, actor.CanManageUsers); err != nil {
		return err
	}
	return requireManageable(ctx, m.db, actor, roleName)
}
