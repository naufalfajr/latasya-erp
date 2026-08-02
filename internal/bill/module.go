package bill

import (
	"database/sql"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/contact"
)

type Module struct {
	db       *sql.DB
	accounts *account.Module
	contacts *contact.Module
}

func New(db *sql.DB) *Module {
	return &Module{db: db, accounts: account.New(db), contacts: contact.New(db)}
}

type Actor struct {
	UserID    int
	CanManage bool
}

func requireManager(actor Actor) error {
	if actor.UserID <= 0 || !actor.CanManage {
		return ErrForbidden
	}
	return nil
}
