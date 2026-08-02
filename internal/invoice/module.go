package invoice

import (
	"database/sql"
	"sync"
	"time"
)

type Module struct {
	db         *sql.DB
	now        func() time.Time
	bulkSendMu sync.Mutex
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
