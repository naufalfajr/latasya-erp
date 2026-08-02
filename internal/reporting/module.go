package reporting

import "database/sql"

type Module struct{ db *sql.DB }

func New(db *sql.DB) *Module { return &Module{db: db} }
