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
	"credit_notes":    "cn_number",
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

	// Take MAX of the trailing sequence number, not COUNT(*). COUNT breaks when
	// a document is deleted mid-sequence: the count drops and the next number
	// collides with a surviving row, violating the UNIQUE constraint. SUBSTR at
	// len(fullPrefix)+2 skips the "PREFIX-YYYYMM-" portion (1-indexed). table and
	// column are validated against the allowlist above.
	var maxNum int
	err := db.QueryRow(
		fmt.Sprintf("SELECT COALESCE(MAX(CAST(SUBSTR(%s, ?) AS INTEGER)), 0) FROM %s WHERE %s LIKE ?", column, table, column),
		len(fullPrefix)+2,
		fullPrefix+"-%",
	).Scan(&maxNum)
	if err != nil {
		return "", fmt.Errorf("max %s: %w", table, err)
	}
	return fmt.Sprintf("%s-%04d", fullPrefix, maxNum+1), nil
}
