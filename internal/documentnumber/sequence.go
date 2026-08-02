package documentnumber

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Kind string

const (
	JournalEntry Kind = "journal_entry"
	Invoice      Kind = "invoice"
	Bill         Kind = "bill"
	CreditNote   Kind = "credit_note"
)

type target struct{ table, column, prefix string }

var targets = map[Kind]target{
	JournalEntry: {table: "journal_entries", column: "reference", prefix: "JE"},
	Invoice:      {table: "invoices", column: "invoice_number", prefix: "INV"},
	Bill:         {table: "bills", column: "bill_number", prefix: "BILL"},
	CreditNote:   {table: "credit_notes", column: "cn_number", prefix: "CN"},
}

type docNumberQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Next(ctx context.Context, db docNumberQueryer, kind Kind) (string, error) {
	return NextAt(ctx, db, kind, time.Now())
}

// NextAt atomically claims the next monthly sequence at the supplied clock.
func NextAt(ctx context.Context, db docNumberQueryer, kind Kind, now time.Time) (string, error) {
	target, ok := targets[kind]
	if !ok {
		return "", fmt.Errorf("invalid document number kind %q", kind)
	}
	fullPrefix := fmt.Sprintf("%s-%s", target.prefix, now.Format("200601"))

	// The insert seeds from existing documents during upgrade; subsequent calls
	// increment the sequence row atomically across DB handles and processes.
	var next int
	err := db.QueryRowContext(ctx, fmt.Sprintf(`INSERT INTO document_sequences (document_type, period, last_number)
		VALUES (?, ?, (SELECT COALESCE(MAX(CAST(SUBSTR(%s, ?) AS INTEGER)), 0) + 1 FROM %s WHERE %s LIKE ?))
		ON CONFLICT(document_type, period) DO UPDATE SET last_number=last_number+1
		RETURNING last_number`, target.column, target.table, target.column), target.table, now.Format("200601"), len(fullPrefix)+2, fullPrefix+"-%").Scan(&next)
	if err != nil {
		return "", fmt.Errorf("next %s number: %w", kind, err)
	}
	return fmt.Sprintf("%s-%04d", fullPrefix, next), nil
}
