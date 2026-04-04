package model

import (
	"database/sql"
	"fmt"
	"time"
)

// Valid table/column pairs for document number generation (prevents SQL injection).
var validDocNumberTargets = map[string]string{
	"journal_entries": "reference",
	"invoices":        "invoice_number",
	"bills":           "bill_number",
}

// GenerateDocNumber generates a sequential document number like PREFIX-YYYYMM-0001.
func GenerateDocNumber(db *sql.DB, table, column, prefix string) (string, error) {
	// Validate table/column against allowlist
	expectedCol, ok := validDocNumberTargets[table]
	if !ok || expectedCol != column {
		return "", fmt.Errorf("invalid document number target: %s.%s", table, column)
	}

	now := time.Now()
	fullPrefix := fmt.Sprintf("%s-%s", prefix, now.Format("200601"))

	var count int
	// table and column are validated against the allowlist above
	err := db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s LIKE ?", table, column),
		fullPrefix+"%",
	).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("count %s: %w", table, err)
	}
	return fmt.Sprintf("%s-%04d", fullPrefix, count+1), nil
}
