package reporting

import (
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("reporting: not found")

type Module struct{ db *sql.DB }

func New(db *sql.DB) *Module { return &Module{db: db} }
