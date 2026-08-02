package invoice

import (
	"database/sql"
	"sync"
	"time"

	"github.com/naufal/latasya-erp/internal/account"
	"github.com/naufal/latasya-erp/internal/company"
	"github.com/naufal/latasya-erp/internal/contact"
	"github.com/naufal/latasya-erp/internal/creditnote"
	"github.com/naufal/latasya-erp/internal/schoolcalendar"
)

type Module struct {
	db          *sql.DB
	now         func() time.Time
	bulkSendMu  sync.Mutex
	creditNotes *creditnote.Module
	accounts    *account.Module
	contacts    *contact.Module
	company     *company.Module
	calendar    *schoolcalendar.Module
}

func New(db *sql.DB) *Module {
	return &Module{db: db, now: time.Now, creditNotes: creditnote.New(db), accounts: account.New(db), contacts: contact.New(db), company: company.New(db), calendar: schoolcalendar.New(db)}
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
