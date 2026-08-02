package invoice

import (
	"database/sql"
	"sync"
	"time"

	"github.com/naufal/latasya-erp/internal/creditnote"
)

type Module struct {
	db          *sql.DB
	now         func() time.Time
	bulkSendMu  sync.Mutex
	creditNotes *creditnote.Module
}

func New(db *sql.DB) *Module {
	return &Module{db: db, now: time.Now, creditNotes: creditnote.New(db)}
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
