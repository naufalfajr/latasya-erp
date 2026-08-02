package access

import "database/sql"

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
}

func require(actor Actor, allowed bool) error {
	if actor.UserID <= 0 || !allowed {
		return ErrForbidden
	}
	return nil
}
