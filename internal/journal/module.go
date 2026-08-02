package journal

import (
	"database/sql"

	"github.com/naufal/latasya-erp/internal/account"
)

type Module struct {
	db       *sql.DB
	accounts *account.Module
}

func New(db *sql.DB) *Module { return &Module{db: db, accounts: account.New(db)} }

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
