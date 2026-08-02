package creditnote

import "database/sql"

type Module struct{ db *sql.DB }

func New(db *sql.DB) *Module { return &Module{db: db} }

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
