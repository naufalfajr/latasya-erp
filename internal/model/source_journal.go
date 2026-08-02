package model

import (
	"database/sql"
	"fmt"
)

// createSourceJournalEntry is transitional persistence for bill and credit-note
// transactions. It is private so transports cannot bypass their domain rules.
func createSourceJournalEntry(db *sql.DB, entry *JournalEntry, lines []JournalLine) (int, error) {
	var debit, credit int
	for i, line := range lines {
		if line.AccountID <= 0 || line.Debit < 0 || line.Credit < 0 || (line.Debit == 0 && line.Credit == 0) || (line.Debit > 0 && line.Credit > 0) {
			return 0, fmt.Errorf("invalid source journal line %d", i)
		}
		debit += line.Debit
		credit += line.Credit
	}
	if debit == 0 || debit != credit {
		return 0, fmt.Errorf("journal entry must be balanced and non-zero")
	}
	if entry.Reference == "" {
		reference, err := GenerateDocNumber(db, "journal_entries", "reference", "JE")
		if err != nil {
			return 0, err
		}
		entry.Reference = reference
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin source journal: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO journal_entries
		(entry_date, reference, description, source_type, source_id, is_posted, vehicle_id, created_by)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?,0), ?)`, entry.EntryDate, entry.Reference, entry.Description,
		entry.SourceType, entry.SourceID, entry.IsPosted, entry.VehicleID, entry.CreatedBy)
	if err != nil {
		return 0, fmt.Errorf("insert source journal: %w", err)
	}
	id64, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("source journal id: %w", err)
	}
	id := int(id64)
	for _, line := range lines {
		if _, err := tx.Exec(`INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo) VALUES (?, ?, ?, ?, ?)`,
			id, line.AccountID, line.Debit, line.Credit, line.Memo); err != nil {
			return 0, fmt.Errorf("insert source journal line: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit source journal: %w", err)
	}
	return id, nil
}
