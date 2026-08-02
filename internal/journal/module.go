package journal

import (
	"database/sql"
)

type Module struct {
	db *sql.DB
}

func New(db *sql.DB) *Module { return &Module{db: db} }

type Actor struct {
	UserID            int
	CanManageJournals bool
	CanManageIncome   bool
	CanManageExpenses bool
}

func require(actor Actor, allowed bool) error {
	if actor.UserID <= 0 || !allowed {
		return ErrForbidden
	}
	return nil
}
