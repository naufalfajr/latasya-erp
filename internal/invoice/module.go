package invoice

import (
	"database/sql"
	"time"
)

type Module struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Module {
	return &Module{db: db, now: time.Now}
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
