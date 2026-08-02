package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Valid table/column pairs for document number generation (prevents SQL injection).
var validDocNumberTargets = map[string]string{
	"journal_entries": "reference",
	"invoices":        "invoice_number",
	"bills":           "bill_number",
	"credit_notes":    "cn_number",
}

// GenerateDocNumber generates a sequential document number like PREFIX-YYYYMM-0001.
func GenerateDocNumber(db *sql.DB, table, column, prefix string) (string, error) {
	return GenerateDocNumberContext(context.Background(), db, table, column, prefix)
}

type docNumberQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// GenerateDocNumberContext atomically claims the next monthly sequence.
func GenerateDocNumberContext(ctx context.Context, db docNumberQueryer, table, column, prefix string) (string, error) {
	// Validate table/column against allowlist
	expectedCol, ok := validDocNumberTargets[table]
	if !ok || expectedCol != column {
		return "", fmt.Errorf("invalid document number target: %s.%s", table, column)
	}

	now := time.Now()
	fullPrefix := fmt.Sprintf("%s-%s", prefix, now.Format("200601"))

	// The insert seeds from existing documents during upgrade; subsequent calls
	// increment the sequence row atomically across DB handles and processes.
	var next int
	err := db.QueryRowContext(ctx, fmt.Sprintf(`INSERT INTO document_sequences (document_type, period, last_number)
		VALUES (?, ?, (SELECT COALESCE(MAX(CAST(SUBSTR(%s, ?) AS INTEGER)), 0) + 1 FROM %s WHERE %s LIKE ?))
		ON CONFLICT(document_type, period) DO UPDATE SET last_number=last_number+1
		RETURNING last_number`, column, table, column), table, now.Format("200601"), len(fullPrefix)+2, fullPrefix+"-%").Scan(&next)
	if err != nil {
		return "", fmt.Errorf("next %s number: %w", table, err)
	}
	return fmt.Sprintf("%s-%04d", fullPrefix, next), nil
}
