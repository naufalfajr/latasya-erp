package model

import (
	"database/sql"
	"fmt"
	"time"
)

type JournalEntry struct {
	ID          int
	EntryDate   string
	Reference   string
	Description string
	SourceType  string
	SourceID    *int
	IsPosted    bool
	CreatedBy   int
	CreatedAt   string
	UpdatedAt   string
	// Joined fields
	Lines         []JournalLine
	CreatedByName string
	TotalDebit    int
	TotalCredit   int
}

type JournalLine struct {
	ID        int
	EntryID   int
	AccountID int
	Debit     int
	Credit    int
	Memo      string
	// Joined fields
	AccountCode string
	AccountName string
}

type JournalFilter struct {
	DateFrom   string
	DateTo     string
	SourceType string
	Search     string
}

// GenerateReference creates a reference like JE-202604-0001
func GenerateReference(db *sql.DB) (string, error) {
	now := time.Now()
	prefix := fmt.Sprintf("JE-%s", now.Format("200601"))

	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM journal_entries WHERE reference LIKE ?",
		prefix+"%",
	).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("count references: %w", err)
	}
	return fmt.Sprintf("%s-%04d", prefix, count+1), nil
}

func CreateJournalEntry(db *sql.DB, je *JournalEntry, lines []JournalLine) (int, error) {
	// Validate debit = credit
	var totalDebit, totalCredit int
	for _, l := range lines {
		totalDebit += l.Debit
		totalCredit += l.Credit
	}
	if totalDebit != totalCredit {
		return 0, fmt.Errorf("debits (%d) must equal credits (%d)", totalDebit, totalCredit)
	}
	if totalDebit == 0 {
		return 0, fmt.Errorf("journal entry must have at least one debit and credit line")
	}

	// Generate reference before starting transaction (avoids deadlock with single connection)
	if je.Reference == "" {
		ref, err := GenerateReference(db)
		if err != nil {
			return 0, err
		}
		je.Reference = ref
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`INSERT INTO journal_entries (entry_date, reference, description, source_type, source_id, is_posted, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		je.EntryDate, je.Reference, je.Description, je.SourceType, je.SourceID, je.IsPosted, je.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert journal entry: %w", err)
	}

	entryID64, _ := result.LastInsertId()
	entryID := int(entryID64)

	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo) VALUES (?, ?, ?, ?, ?)",
			entryID, l.AccountID, l.Debit, l.Credit, l.Memo,
		)
		if err != nil {
			return 0, fmt.Errorf("insert journal line: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return entryID, nil
}

func GetJournalEntry(db *sql.DB, id int) (*JournalEntry, error) {
	je := &JournalEntry{}
	err := db.QueryRow(
		`SELECT je.id, je.entry_date, COALESCE(je.reference,''), je.description,
			COALESCE(je.source_type,''), je.source_id, je.is_posted, je.created_by,
			je.created_at, je.updated_at, u.full_name
		 FROM journal_entries je
		 JOIN users u ON u.id = je.created_by
		 WHERE je.id = ?`, id,
	).Scan(&je.ID, &je.EntryDate, &je.Reference, &je.Description,
		&je.SourceType, &je.SourceID, &je.IsPosted, &je.CreatedBy,
		&je.CreatedAt, &je.UpdatedAt, &je.CreatedByName)
	if err != nil {
		return nil, fmt.Errorf("get journal entry: %w", err)
	}

	lines, err := getJournalLines(db, id)
	if err != nil {
		return nil, err
	}
	je.Lines = lines

	for _, l := range lines {
		je.TotalDebit += l.Debit
		je.TotalCredit += l.Credit
	}

	return je, nil
}

func getJournalLines(db *sql.DB, entryID int) ([]JournalLine, error) {
	rows, err := db.Query(
		`SELECT jl.id, jl.entry_id, jl.account_id, jl.debit, jl.credit, COALESCE(jl.memo,''),
			a.code, a.name
		 FROM journal_lines jl
		 JOIN accounts a ON a.id = jl.account_id
		 WHERE jl.entry_id = ?
		 ORDER BY jl.id`, entryID,
	)
	if err != nil {
		return nil, fmt.Errorf("get journal lines: %w", err)
	}
	defer rows.Close()

	var lines []JournalLine
	for rows.Next() {
		var l JournalLine
		err := rows.Scan(&l.ID, &l.EntryID, &l.AccountID, &l.Debit, &l.Credit, &l.Memo,
			&l.AccountCode, &l.AccountName)
		if err != nil {
			return nil, fmt.Errorf("scan journal line: %w", err)
		}
		lines = append(lines, l)
	}
	return lines, nil
}

func ListJournalEntries(db *sql.DB, f JournalFilter) ([]JournalEntry, error) {
	query := `SELECT je.id, je.entry_date, COALESCE(je.reference,''), je.description,
			COALESCE(je.source_type,''), je.source_id, je.is_posted, je.created_by,
			je.created_at, je.updated_at, u.full_name,
			COALESCE((SELECT SUM(debit) FROM journal_lines WHERE entry_id = je.id), 0),
			COALESCE((SELECT SUM(credit) FROM journal_lines WHERE entry_id = je.id), 0)
		 FROM journal_entries je
		 JOIN users u ON u.id = je.created_by
		 WHERE 1=1`
	var args []any

	if f.DateFrom != "" {
		query += " AND je.entry_date >= ?"
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		query += " AND je.entry_date <= ?"
		args = append(args, f.DateTo)
	}
	if f.SourceType != "" {
		query += " AND je.source_type = ?"
		args = append(args, f.SourceType)
	}
	if f.Search != "" {
		query += " AND (je.reference LIKE ? OR je.description LIKE ?)"
		s := "%" + f.Search + "%"
		args = append(args, s, s)
	}
	query += " ORDER BY je.entry_date DESC, je.id DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list journal entries: %w", err)
	}
	defer rows.Close()

	var entries []JournalEntry
	for rows.Next() {
		var je JournalEntry
		err := rows.Scan(&je.ID, &je.EntryDate, &je.Reference, &je.Description,
			&je.SourceType, &je.SourceID, &je.IsPosted, &je.CreatedBy,
			&je.CreatedAt, &je.UpdatedAt, &je.CreatedByName,
			&je.TotalDebit, &je.TotalCredit)
		if err != nil {
			return nil, fmt.Errorf("scan journal entry: %w", err)
		}
		entries = append(entries, je)
	}
	return entries, nil
}

func UpdateJournalEntry(db *sql.DB, je *JournalEntry, lines []JournalLine) error {
	// Validate debit = credit
	var totalDebit, totalCredit int
	for _, l := range lines {
		totalDebit += l.Debit
		totalCredit += l.Credit
	}
	if totalDebit != totalCredit {
		return fmt.Errorf("debits (%d) must equal credits (%d)", totalDebit, totalCredit)
	}
	if totalDebit == 0 {
		return fmt.Errorf("journal entry must have at least one debit and credit line")
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`UPDATE journal_entries SET entry_date=?, description=?, updated_at=datetime('now') WHERE id=?`,
		je.EntryDate, je.Description, je.ID,
	)
	if err != nil {
		return fmt.Errorf("update journal entry: %w", err)
	}

	// Delete old lines and insert new ones
	_, err = tx.Exec("DELETE FROM journal_lines WHERE entry_id = ?", je.ID)
	if err != nil {
		return fmt.Errorf("delete old lines: %w", err)
	}

	for _, l := range lines {
		_, err := tx.Exec(
			"INSERT INTO journal_lines (entry_id, account_id, debit, credit, memo) VALUES (?, ?, ?, ?, ?)",
			je.ID, l.AccountID, l.Debit, l.Credit, l.Memo,
		)
		if err != nil {
			return fmt.Errorf("insert journal line: %w", err)
		}
	}

	return tx.Commit()
}

func DeleteJournalEntry(db *sql.DB, id int) error {
	// Only allow deleting manual entries
	var sourceType sql.NullString
	err := db.QueryRow("SELECT source_type FROM journal_entries WHERE id = ?", id).Scan(&sourceType)
	if err != nil {
		return fmt.Errorf("get journal entry: %w", err)
	}
	if sourceType.Valid && sourceType.String != "manual" && sourceType.String != "" {
		return fmt.Errorf("cannot delete auto-generated journal entry (source: %s)", sourceType.String)
	}

	_, err = db.Exec("DELETE FROM journal_entries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete journal entry: %w", err)
	}
	return nil
}
