package model

import (
	"database/sql"
	"fmt"
	"time"
)

// GenerateDocNumber generates a sequential document number like PREFIX-YYYYMM-0001.
func GenerateDocNumber(db *sql.DB, table, column, prefix string) (string, error) {
	now := time.Now()
	fullPrefix := fmt.Sprintf("%s-%s", prefix, now.Format("200601"))

	var count int
	err := db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s LIKE ?", table, column),
		fullPrefix+"%",
	).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("count %s: %w", table, err)
	}
	return fmt.Sprintf("%s-%04d", fullPrefix, count+1), nil
}
